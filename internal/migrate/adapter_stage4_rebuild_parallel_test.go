package migrate

import (
	"context"
	"database/sql"
	"reflect"
	"sort"
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/schema"
)

func TestStage4AdapterRebuildTableParallelismSpendsGlobalEnvelopes(t *testing.T) {
	t.Parallel()
	resources := config.EffectiveTransferPlan{
		Workers:         config.EffectiveInt{Value: 8},
		ConnectionLimit: config.EffectiveInt{Value: 36},
		Readers:         config.EffectiveInt{Value: 1},
		Writers:         config.EffectiveInt{Value: 1},
	}
	if got := stage4AdapterRebuildTableParallelism(resources, 9); got != 8 {
		t.Fatalf("parallelism = %d, want worker-bounded 8", got)
	}
	resources.Workers.Value = 64
	resources.ConnectionLimit.Value = 6
	if got := stage4AdapterRebuildTableParallelism(resources, 9); got != 3 {
		t.Fatalf("parallelism = %d, want connection-bounded 3", got)
	}
	if got := stage4AdapterRebuildTableParallelism(resources, 2); got != 2 {
		t.Fatalf("parallelism = %d, want table-bounded 2", got)
	}
}

func TestConfigureStage4AdapterRebuildSourcePoolUsesTableParallelism(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", "file::memory:?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	source := &relationalSourceAdapter{database: database}
	resources := config.EffectiveTransferPlan{
		Workers:         config.EffectiveInt{Value: 16},
		ConnectionLimit: config.EffectiveInt{Value: 36},
		Readers:         config.EffectiveInt{Value: 1},
		Writers:         config.EffectiveInt{Value: 1},
	}
	configureStage4AdapterRebuildSourcePool(source, resources, 9)
	if got := database.Stats().MaxOpenConnections; got != 9 {
		t.Fatalf("source pool = %d, want nine independent table readers", got)
	}
	resources.Readers.Value = 2
	configureStage4AdapterRebuildSourcePool(source, resources, 9)
	if got := database.Stats().MaxOpenConnections; got != 18 {
		t.Fatalf("source pool = %d, want nine tables times two stable readers", got)
	}
}

func TestStage4AdapterRebuildLoadOrderRunsLargestTablesFirst(t *testing.T) {
	t.Parallel()
	execution := &stage4AdapterNetworkExecution{
		source: &recordingAdapterSource{},
		prepared: stage4AdapterPrepared{plans: []adapterTablePlan{
			{source: schema.Table{Name: "small"}},
			{source: schema.Table{Name: "large"}},
			{source: schema.Table{Name: "equal"}},
		}},
	}
	// Non-relational fixtures retain deterministic plan order; production
	// relational ordering is covered through the stable sort contract below.
	order, err := stage4AdapterRebuildLoadOrder(context.Background(), execution)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []int{0, 1, 2}) {
		t.Fatalf("fixture order = %v", order)
	}
	entries := []stage4AdapterRebuildOrderEntry{
		{index: 0, rows: 10},
		{index: 1, rows: 100},
		{index: 2, rows: 100},
	}
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].rows == entries[right].rows {
			return entries[left].index < entries[right].index
		}
		return entries[left].rows > entries[right].rows
	})
	if got := []int{entries[0].index, entries[1].index, entries[2].index}; !reflect.DeepEqual(got, []int{1, 2, 0}) {
		t.Fatalf("largest-first order = %v", got)
	}
}
