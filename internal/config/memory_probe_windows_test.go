//go:build windows

package config

import (
	"context"
	"testing"
)

func TestWindowsSystemMemoryProbeReturnsFiniteEvidence(t *testing.T) {
	snapshot, err := NewSystemMemoryProbe().ProbeMemory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostCapacityBytes <= 0 || snapshot.HostAvailableBytes <= 0 {
		t.Fatalf("invalid Windows memory evidence: %+v", snapshot)
	}
	if snapshot.ProcessLimit.State == CgroupLimitFinite &&
		(snapshot.ProcessLimit.LimitBytes <= snapshot.ProcessLimit.CurrentBytes) {
		t.Fatalf("invalid Windows Job Object evidence: %+v", snapshot.ProcessLimit)
	}
}
