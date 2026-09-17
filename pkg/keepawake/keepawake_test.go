package keepawake

import "testing"

func TestAcquireRelease(t *testing.T) {
	k := Acquire("test")
	// Should not panic, even if not held
	if k.Held {
		if err := k.Release(); err != nil {
			t.Fatalf("release: %v", err)
		}
		if k.Held {
			t.Fatalf("should not be held after release")
		}
	} else {
		// Not held is okay (e.g., no systemd-inhibit), just check Err is set or not
		_ = k.Err
		_ = k.Release()
	}
}

func TestAcquireNoPanic(t *testing.T) {
	// Multiple acquires should not panic
	k1 := Acquire("test1")
	k2 := Acquire("test2")
	_ = k1
	_ = k2
	_ = k1.Release()
	_ = k2.Release()
}
