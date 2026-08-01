package health

import "testing"

func TestRegistrySetSnapshot(t *testing.T) {
	r := NewRegistry()
	if got := len(r.Snapshot()); got != 0 {
		t.Fatalf("empty registry Snapshot() len = %d, want 0", got)
	}

	r.Set("p1", PipelineStatus{State: StateRunning, Bitrate: "1000kbits/s", FPS: 30})
	snap := r.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("Snapshot() len = %d, want 1", len(snap))
	}
	if snap["p1"].State != StateRunning || snap["p1"].Bitrate != "1000kbits/s" {
		t.Errorf("unexpected snapshot: %+v", snap["p1"])
	}

	// Snapshot must return a copy: mutating the returned struct must not
	// affect the registry.
	cp := snap["p1"]
	cp.State = StateStopped
	if got := r.Snapshot()["p1"].State; got != StateRunning {
		t.Errorf("Snapshot() is not a copy: registry now shows %v", got)
	}

	// Overwrite an existing key.
	r.Set("p1", PipelineStatus{State: StateBackoff})
	if got := r.Snapshot()["p1"].State; got != StateBackoff {
		t.Errorf("Set() overwrite failed: %v", got)
	}
}
