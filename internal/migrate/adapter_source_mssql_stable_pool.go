package migrate

import (
	"context"
	"fmt"
	"sync"

	"github.com/johndauphine/dmtx/internal/schema"
)

// adapterPooledStableNetworkSource retains the primary session for catalog and
// pagination operations while distributing immutable range reads across
// sibling stable sessions.
type adapterPooledStableNetworkSource struct {
	adapterStableNetworkSource
	readers []*adapterLockedStableNetworkReader
}

type adapterLockedStableNetworkReader struct {
	mu     sync.Mutex
	source adapterNetworkRangePageSource
}

func (source *adapterPooledStableNetworkSource) ReadNetworkRangePage(
	ctx context.Context,
	table schema.Table,
	columns []string,
	pagination PaginationPlan,
	plannedRange PaginationRange,
	request NetworkReadRequest,
) (NetworkReadPage, error) {
	if source == nil || len(source.readers) == 0 {
		return NetworkReadPage{}, fmt.Errorf("stable source reader pool is unavailable")
	}
	reader := source.readers[request.Range.RangeIndex%uint64(len(source.readers))]
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.source.ReadNetworkRangePage(
		ctx,
		table,
		columns,
		pagination,
		plannedRange,
		request,
	)
}

// composeSQLServerStableReaderPool opens sibling SERIALIZABLE sessions only
// after the primary session has acquired TABLOCK/HOLDLOCK. The primary lock
// prevents source writes while every sibling proves the same pagination and
// retained-row evidence, yielding bounded parallel reads without snapshot
// drift.
func composeSQLServerStableReaderPool(
	ctx context.Context,
	source sourceAdapter,
	table schema.Table,
	columns []string,
	partitions int,
	pagination PaginationPlan,
	evidence RuntimeRowWidthEvidence,
	primary *adapterStableNetworkTableSession,
	stable adapterStableNetworkSource,
	requestedReaders int,
) (adapterStableNetworkSource, int, error) {
	if source == nil || source.Engine() != "mssql" ||
		requestedReaders <= 1 || len(pagination.Ranges) <= 1 {
		return stable, primary.ReaderLimit(), nil
	}
	readerCount := requestedReaders
	if readerCount > len(pagination.Ranges) {
		readerCount = len(pagination.Ranges)
	}
	readers := make([]*adapterLockedStableNetworkReader, 0, readerCount)
	readers = append(readers, &adapterLockedStableNetworkReader{source: stable})
	for len(readers) < readerCount {
		sibling, err := OpenAdapterStableNetworkTableSource(ctx, source, table)
		if err != nil {
			return nil, 0, fmt.Errorf("open SQL Server stable source reader %d: %w", len(readers)+1, err)
		}
		primary.children = append(primary.children, sibling)
		siblingSource, err := sibling.Source()
		if err != nil {
			return nil, 0, err
		}
		// The primary session already holds TABLOCK/HOLDLOCK, so neither row
		// contents nor schema can change while siblings are open. Reuse its
		// pagination and retained-width proofs instead of forcing every reader to
		// rescan the full table before useful work begins.
		if err := inheritStage4SQLServerStrictLockProofs(
			stable,
			siblingSource,
		); err != nil {
			return nil, 0, fmt.Errorf(
				"inherit SQL Server stable reader proofs: %w",
				err,
			)
		}
		readers = append(readers, &adapterLockedStableNetworkReader{source: siblingSource})
	}
	return &adapterPooledStableNetworkSource{
		adapterStableNetworkSource: stable,
		readers:                    readers,
	}, len(readers), nil
}
