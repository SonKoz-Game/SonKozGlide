package instance

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func testName(t *testing.T) string {
	return fmt.Sprintf("SonKozGlide_InstanceTest_%d_%s", os.Getpid(), t.Name())
}

func TestAcquireRefusesASecondInstance(t *testing.T) {
	name := testName(t)
	first, ok := Acquire(name, 0)
	if !ok {
		t.Fatal("the first instance must get the lock")
	}
	defer windows.CloseHandle(first)

	if _, ok := Acquire(name, 0); ok {
		t.Fatal("a second instance must not get the lock while the first is running")
	}
}

func TestAcquireWaitsForTheRestartingInstanceToExit(t *testing.T) {
	name := testName(t)
	first, ok := Acquire(name, 0)
	if !ok {
		t.Fatal("the first instance must get the lock")
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		windows.CloseHandle(first)
	}()

	start := time.Now()
	second, ok := Acquire(name, 3*time.Second)
	if !ok {
		t.Fatal("the restarted instance must get the lock once the old one exits")
	}
	defer windows.CloseHandle(second)
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("expected to wait for the old instance, waited %s", elapsed)
	}

	if _, ok := Acquire(name, 0); ok {
		t.Fatal("the restarted instance must hold the lock")
	}
}

func TestAcquireGivesUpAfterTheWait(t *testing.T) {
	name := testName(t)
	first, ok := Acquire(name, 0)
	if !ok {
		t.Fatal("the first instance must get the lock")
	}
	defer windows.CloseHandle(first)

	start := time.Now()
	if _, ok := Acquire(name, 300*time.Millisecond); ok {
		t.Fatal("the lock is still held")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("gave up too late: %s", elapsed)
	}
}
