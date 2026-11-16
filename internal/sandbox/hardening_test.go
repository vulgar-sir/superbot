package sandbox

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestBoundedBufferUnderLimit(t *testing.T) {
	b := newBoundedBuffer(16)
	if n, err := b.Write([]byte("hello")); n != 5 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if b.Truncated() || b.Dropped() != 0 {
		t.Fatalf("unexpected truncation: dropped=%d", b.Dropped())
	}
	if got := b.String(); got != "hello" {
		t.Fatalf("String = %q", got)
	}
}

func TestBoundedBufferExactLimit(t *testing.T) {
	b := newBoundedBuffer(5)
	_, _ = b.Write([]byte("hello"))
	if b.Truncated() || b.String() != "hello" {
		t.Fatalf("exact fit must not truncate: %q", b.String())
	}
}

func TestBoundedBufferCapsMemoryAndCountsDropped(t *testing.T) {
	const limit = 1024
	b := newBoundedBuffer(limit)
	chunk := []byte(strings.Repeat("x", 300))
	total := 0
	for i := 0; i < 1000; i++ {
		n, err := b.Write(chunk)
		if n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v; a short write would stall the producer", n, err)
		}
		total += n
	}
	if len(b.buf) != limit || cap(b.buf) > 4*limit {
		t.Fatalf("kept len=%d cap=%d, want len %d and bounded cap", len(b.buf), cap(b.buf), limit)
	}
	if want := int64(total - limit); b.Dropped() != want {
		t.Fatalf("Dropped = %d, want %d", b.Dropped(), want)
	}
	got := b.String()
	if !strings.HasPrefix(got, strings.Repeat("x", limit)+"\n[output truncated: ") {
		t.Fatalf("missing kept prefix or notice: %q", got[len(got)-80:])
	}
	if !strings.Contains(got, "298976 bytes dropped, limit 1024 bytes]") {
		t.Fatalf("notice lacks counts: %q", got[limit:])
	}
}

func TestBoundedBufferSplitWrite(t *testing.T) {
	b := newBoundedBuffer(4)
	_, _ = b.Write([]byte("ab"))
	_, _ = b.Write([]byte("cdef"))
	_, _ = b.Write([]byte("gh"))
	if string(b.buf) != "abcd" || b.Dropped() != 4 {
		t.Fatalf("buf=%q dropped=%d", b.buf, b.Dropped())
	}
}

func TestBoundedBufferDoesNotSplitRune(t *testing.T) {
	b := newBoundedBuffer(4)
	_, _ = b.Write([]byte("ab中文")) // "中" is 3 bytes; the cut lands inside it.
	got := b.String()
	if !strings.HasPrefix(got, "ab\n[output truncated: 6 bytes dropped") {
		t.Fatalf("String = %q", got)
	}
}

func TestBoundedBufferZeroLimit(t *testing.T) {
	b := newBoundedBuffer(-1)
	_, _ = b.Write([]byte("abc"))
	if len(b.buf) != 0 || b.Dropped() != 3 {
		t.Fatalf("buf=%q dropped=%d", b.buf, b.Dropped())
	}
}

func TestKeyedMutexExcludesWithinKey(t *testing.T) {
	var locks keyedMutex[string]
	var inside, maxInside, counter int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := locks.Lock("bot")
			defer unlock()
			now := atomic.AddInt32(&inside, 1)
			for {
				seen := atomic.LoadInt32(&maxInside)
				if now <= seen || atomic.CompareAndSwapInt32(&maxInside, seen, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			counter++ // unsynchronized on purpose: the race detector checks exclusion.
			atomic.AddInt32(&inside, -1)
		}()
	}
	wg.Wait()
	if maxInside != 1 {
		t.Fatalf("%d goroutines held the same key at once", maxInside)
	}
	if counter != 32 {
		t.Fatalf("counter = %d, want 32", counter)
	}
}

func TestKeyedMutexParallelAcrossKeys(t *testing.T) {
	var locks keyedMutex[string]
	unlockA := locks.Lock("a")
	defer unlockA()

	acquired := make(chan struct{})
	go func() {
		unlock := locks.Lock("b")
		close(acquired)
		unlock()
	}()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("key b blocked behind key a")
	}

	blocked := make(chan struct{})
	go func() {
		unlock := locks.Lock("a")
		close(blocked)
		unlock()
	}()
	select {
	case <-blocked:
		t.Fatal("second holder of key a got in while it was held")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestKeyedMutexReusableAfterUnlock(t *testing.T) {
	var locks keyedMutex[int]
	for i := 0; i < 3; i++ {
		locks.Lock(7)()
	}
	if len(locks.locks) != 1 {
		t.Fatalf("entries = %d, want 1", len(locks.locks))
	}
}

func TestExecKillCommand(t *testing.T) {
	cmd := execKillCommand("abc-123")
	if len(cmd) != 5 || cmd[0] != "sh" || cmd[1] != "-c" || cmd[2] != execKillScript {
		t.Fatalf("unexpected command: %q", cmd)
	}
	// The marker must travel as an argument: in the script text or the
	// killer's environment it would make the killer match itself.
	if cmd[4] != "SUPERBOT_EXEC_MARK=abc-123" {
		t.Fatalf("marker arg = %q", cmd[4])
	}
	if strings.Contains(execKillScript, "abc-123") {
		t.Fatal("marker leaked into script text")
	}
}

// Runs the real kill script against a marked process tree. Needs a Linux
// /proc, so it is skipped elsewhere.
func TestExecKillScriptKillsMarkedTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs Linux /proc")
	}
	marked := exec.Command("sh", "-c", "sleep 60 & sleep 60 & wait")
	marked.Env = append(os.Environ(), execMarker("kill-test"))
	if err := marked.Start(); err != nil {
		t.Fatalf("start marked: %v", err)
	}
	other := exec.Command("sleep", "60")
	other.Env = append(os.Environ(), execMarker("someone-else"))
	if err := other.Start(); err != nil {
		t.Fatalf("start other: %v", err)
	}
	defer func() { _ = other.Process.Kill(); _ = other.Wait() }()
	time.Sleep(200 * time.Millisecond)

	kill := execKillCommand("kill-test")
	if out, err := exec.Command(kill[0], kill[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("kill script: %v: %s", err, out)
	}
	done := make(chan error, 1)
	go func() { done <- marked.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = marked.Process.Kill()
		t.Fatal("marked process survived the kill script")
	}
	if err := other.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unmarked process was killed: %v", err)
	}
}
