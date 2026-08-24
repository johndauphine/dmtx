package migrate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/johndauphine/dmtx/internal/config"
)

type stage4AdapterRebuildOrderEntry struct {
	index int
	rows  int
}

type stage4AdapterRebuildTransferResult struct {
	index int
	rows  int
	work  stage4AdapterWork
	err   error
}

// stage4AdapterRebuildTableParallelism admits independent table pipelines.
// Rebuild targets carry no foreign keys until finalization, so every table is
// eligible immediately. Readers and writers are per-table-job settings (the
// same contract used by DMT), so every admitted job spends that complete
// connection set. The table-worker and connection budgets therefore bound the
// largest safe no-FK wave without a second global gate throttling useful work.
func stage4AdapterRebuildTableParallelism(
	resources config.EffectiveTransferPlan,
	tables int,
) int {
	if tables < 1 {
		return 1
	}
	parallelism := tables
	if limit := resources.Workers.Value; limit < parallelism {
		parallelism = limit
	}
	connectionsPerTable := resources.Readers.Value + resources.Writers.Value
	if connectionsPerTable < 1 {
		connectionsPerTable = 1
	}
	if limit := resources.ConnectionLimit.Value / connectionsPerTable; limit < parallelism {
		parallelism = limit
	}
	if parallelism < 1 {
		parallelism = 1
	}
	return parallelism
}

// configureStage4AdapterRebuildSourcePool expands the discovery-time
// single-connection relational pool only after schema admission is complete.
// Each concurrently loaded table owns one stable source transaction, so the
// scheduler's table bound is also the exact source-connection bound.
func configureStage4AdapterRebuildSourcePool(
	source sourceAdapter,
	resources config.EffectiveTransferPlan,
	tables int,
) {
	relational, ok := source.(*relationalSourceAdapter)
	if !ok || relational == nil || relational.database == nil {
		return
	}
	parallelism := stage4AdapterRebuildTableParallelism(resources, tables)
	connections := parallelism * resources.Readers.Value
	if connections < parallelism {
		connections = parallelism
	}
	relational.database.SetMaxOpenConns(connections)
	relational.database.SetMaxIdleConns(connections)
}

func stage4AdapterRebuildLoadOrder(
	ctx context.Context,
	execution *stage4AdapterNetworkExecution,
) ([]int, error) {
	entries := make([]stage4AdapterRebuildOrderEntry, len(execution.prepared.plans))
	for index := range entries {
		entries[index].index = index
	}
	// Exact counts are cheap on the certified relational sources and make the
	// bounded scheduler longest-processing-time-first. If counts change before
	// resume, the stable pagination topology changes too and replay already
	// fails closed; the order therefore cannot silently rebind durable work.
	if _, ok := execution.source.(*relationalSourceAdapter); ok {
		var counts sync.WaitGroup
		errorsByIndex := make([]error, len(execution.prepared.plans))
		for index, plan := range execution.prepared.plans {
			index, plan := index, plan
			counts.Add(1)
			go func() {
				defer counts.Done()
				rows, err := execution.source.CountRows(ctx, plan.source)
				if err != nil {
					errorsByIndex[index] = fmt.Errorf(
						"count Stage 4 rebuild scheduling rows for %s: %w",
						plan.source.Name,
						err,
					)
					return
				}
				entries[index].rows = rows
			}()
		}
		counts.Wait()
		for _, countErr := range errorsByIndex {
			if countErr != nil {
				return nil, countErr
			}
		}
	}
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].rows == entries[right].rows {
			return entries[left].index < entries[right].index
		}
		return entries[left].rows > entries[right].rows
	})
	order := make([]int, len(entries))
	for index, entry := range entries {
		order[index] = entry.index
	}
	return order, nil
}

// runStage4AdapterParallelNetworkRebuildTables opens stable source views in
// deterministic plan order, then executes all load-time-independent rebuild
// tables concurrently within the migration's worker, connection, and memory
// envelopes. openError is used by the fresh no-recovery route to retain its
// rerun-required classification.
func runStage4AdapterParallelNetworkRebuildTables(
	ctx context.Context,
	observer TableObserver,
	execution *stage4AdapterNetworkExecution,
	openError func(error) error,
) ([]int, []stage4AdapterWork, error) {
	if execution == nil {
		return nil, nil, fmt.Errorf("Stage 4 network execution is unavailable")
	}
	releaseFence := func() error { return nil }
	if fencer, ok := execution.target.(adapterStage4NetworkRebuildSetFencer); ok && !isNilInterface(fencer) {
		var err error
		releaseFence, err = fencer.BeginStage4NetworkRebuildSetFence(
			ctx,
			stage4AdapterRebuildTargetTables(execution.prepared),
		)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"establish Stage 4 rebuild target-set fence: %w",
				err,
			)
		}
		if releaseFence == nil {
			return nil, nil, NewTransferError(
				ErrorClassState,
				fmt.Errorf("Stage 4 rebuild target-set fence has no release boundary"),
			)
		}
	}
	copiedRows, boundWork, runErr :=
		runStage4AdapterParallelNetworkRebuildTablesUnfenced(
			ctx,
			observer,
			execution,
			openError,
		)
	releaseErr := releaseFence()
	if releaseErr != nil {
		releaseErr = fmt.Errorf(
			"release Stage 4 rebuild target-set fence: %w",
			releaseErr,
		)
	}
	return copiedRows, boundWork, errors.Join(runErr, releaseErr)
}

func runStage4AdapterParallelNetworkRebuildTablesUnfenced(
	ctx context.Context,
	observer TableObserver,
	execution *stage4AdapterNetworkExecution,
	openError func(error) error,
) ([]int, []stage4AdapterWork, error) {
	if execution == nil {
		return nil, nil, fmt.Errorf("Stage 4 network execution is unavailable")
	}
	tableCount := len(execution.prepared.plans)
	copiedRows := make([]int, tableCount)
	boundWork := make([]stage4AdapterWork, tableCount)
	if tableCount == 0 {
		return copiedRows, boundWork, nil
	}
	budget, err := NewByteBudget(execution.resources.MemoryBudget.Value)
	if err != nil {
		return nil, nil, err
	}
	parallelism := stage4AdapterRebuildTableParallelism(
		execution.resources,
		tableCount,
	)
	// SQLite has one database-wide writer and may share that file with durable
	// state in embedded deployments. Independent table goroutines would only
	// contend on SQLITE_BUSY; retain the engine's certified single-writer path.
	if execution.target != nil && execution.target.Engine() == "sqlite" {
		parallelism = 1
	}
	loadOrder, err := stage4AdapterRebuildLoadOrder(ctx, execution)
	if err != nil {
		return nil, nil, err
	}
	pipelineCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	gate := make(chan struct{}, parallelism)
	results := make(chan stage4AdapterRebuildTransferResult, tableCount)
	var workers sync.WaitGroup
	var failureMu sync.Mutex
	var firstFailure error
	fail := func(value error) {
		if value == nil {
			return
		}
		failureMu.Lock()
		if firstFailure == nil {
			firstFailure = value
			cancel()
		}
		failureMu.Unlock()
	}

dispatch:
	for _, planIndex := range loadOrder {
		select {
		case gate <- struct{}{}:
		case <-pipelineCtx.Done():
			break dispatch
		}
		tableExecution, openErr := execution.openTable(
			pipelineCtx,
			planIndex,
			false,
		)
		if openErr != nil {
			<-gate
			if openError != nil {
				openErr = openError(openErr)
			}
			fail(openErr)
			break
		}
		tableExecution.corePlan.SharedBudget = budget
		work := cloneStage4AdapterNetworkWork(tableExecution.work)
		workers.Add(1)
		go func(
			index int,
			table *stage4AdapterNetworkTableExecution,
			bound stage4AdapterWork,
		) {
			defer workers.Done()
			defer func() { <-gate }()
			rows, runErr := runStage4AdapterStableNetworkRebuildTableData(
				pipelineCtx,
				observer,
				table,
			)
			results <- stage4AdapterRebuildTransferResult{
				index: index,
				rows:  rows,
				work:  bound,
				err:   runErr,
			}
			fail(runErr)
		}(planIndex, tableExecution, work)
	}
	workers.Wait()
	close(results)
	for result := range results {
		copiedRows[result.index] = result.rows
		boundWork[result.index] = result.work
	}
	failureMu.Lock()
	err = firstFailure
	failureMu.Unlock()
	if stats := budget.Stats(); stats.Current != 0 {
		leak := NewTransferError(
			ErrorClassState,
			fmt.Errorf("parallel Stage 4 rebuild leaked %d admitted bytes", stats.Current),
		)
		if err != nil {
			return nil, nil, errors.Join(err, leak)
		}
		return nil, nil, leak
	}
	if err != nil {
		return nil, nil, err
	}
	return copiedRows, boundWork, nil
}
