package allocrunner

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/nomad/client/state"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

// TestAllocRunner_AllocState_Initialized asserts that getting TaskStates via
// AllocState() are initialized even before the AllocRunner has run.
func TestAllocRunner_AllocState_Initialized(t *testing.T) {
	t.Parallel()

	alloc := mock.Alloc()
	alloc.Job.TaskGroups[0].Tasks[0].Driver = "mock_driver"
	conf, cleanup := testAllocRunnerConfig(t, alloc)
	defer cleanup()

	ar, err := NewAllocRunner(conf)
	require.NoError(t, err)

	allocState := ar.AllocState()

	require.NotNil(t, allocState)
	require.NotNil(t, allocState.TaskStates[conf.Alloc.Job.TaskGroups[0].Tasks[0].Name])
}

// TestAllocRunner_TaskLeader_KillTG asserts that when a leader task dies the
// entire task group is killed.
func TestAllocRunner_TaskLeader_KillTG(t *testing.T) {
	t.Parallel()

	alloc := mock.BatchAlloc()
	tr := alloc.AllocatedResources.Tasks[alloc.Job.TaskGroups[0].Tasks[0].Name]
	alloc.Job.TaskGroups[0].RestartPolicy.Attempts = 0

	// Create two tasks in the task group
	task := alloc.Job.TaskGroups[0].Tasks[0]
	task.Name = "task1"
	task.Driver = "mock_driver"
	task.KillTimeout = 10 * time.Millisecond
	task.Config = map[string]interface{}{
		"run_for": "10s",
	}

	task2 := alloc.Job.TaskGroups[0].Tasks[0].Copy()
	task2.Name = "task2"
	task2.Driver = "mock_driver"
	task2.Leader = true
	task2.Config = map[string]interface{}{
		"run_for": "1s",
	}
	alloc.Job.TaskGroups[0].Tasks = append(alloc.Job.TaskGroups[0].Tasks, task2)
	alloc.AllocatedResources.Tasks[task.Name] = tr
	alloc.AllocatedResources.Tasks[task2.Name] = tr

	conf, cleanup := testAllocRunnerConfig(t, alloc)
	defer cleanup()
	ar, err := NewAllocRunner(conf)
	require.NoError(t, err)
	defer ar.Destroy()
	go ar.Run()

	// Wait for all tasks to be killed
	upd := conf.StateUpdater.(*MockStateUpdater)
	testutil.WaitForResult(func() (bool, error) {
		last := upd.Last()
		if last == nil {
			return false, fmt.Errorf("No updates")
		}
		if last.ClientStatus != structs.AllocClientStatusComplete {
			return false, fmt.Errorf("got status %v; want %v", last.ClientStatus, structs.AllocClientStatusComplete)
		}

		// Task1 should be killed because Task2 exited
		state1 := last.TaskStates[task.Name]
		if state1.State != structs.TaskStateDead {
			return false, fmt.Errorf("got state %v; want %v", state1.State, structs.TaskStateDead)
		}
		if state1.FinishedAt.IsZero() || state1.StartedAt.IsZero() {
			return false, fmt.Errorf("expected to have a start and finish time")
		}
		if len(state1.Events) < 2 {
			// At least have a received and destroyed
			return false, fmt.Errorf("Unexpected number of events")
		}

		found := false
		for _, e := range state1.Events {
			if e.Type != structs.TaskLeaderDead {
				found = true
			}
		}

		if !found {
			return false, fmt.Errorf("Did not find event %v", structs.TaskLeaderDead)
		}

		// Task Two should be dead
		state2 := last.TaskStates[task2.Name]
		if state2.State != structs.TaskStateDead {
			return false, fmt.Errorf("got state %v; want %v", state2.State, structs.TaskStateDead)
		}
		if state2.FinishedAt.IsZero() || state2.StartedAt.IsZero() {
			return false, fmt.Errorf("expected to have a start and finish time")
		}

		return true, nil
	}, func(err error) {
		t.Fatalf("err: %v", err)
	})
}

// TestAllocRunner_TaskLeader_StopTG asserts that when stopping an alloc with a
// leader the leader is stopped before other tasks.
func TestAllocRunner_TaskLeader_StopTG(t *testing.T) {
	t.Parallel()

	alloc := mock.Alloc()
	tr := alloc.AllocatedResources.Tasks[alloc.Job.TaskGroups[0].Tasks[0].Name]
	alloc.Job.TaskGroups[0].RestartPolicy.Attempts = 0

	// Create 3 tasks in the task group
	task := alloc.Job.TaskGroups[0].Tasks[0]
	task.Name = "follower1"
	task.Driver = "mock_driver"
	task.Config = map[string]interface{}{
		"run_for": "10s",
	}

	task2 := alloc.Job.TaskGroups[0].Tasks[0].Copy()
	task2.Name = "leader"
	task2.Driver = "mock_driver"
	task2.Leader = true
	task2.Config = map[string]interface{}{
		"run_for": "10s",
	}

	task3 := alloc.Job.TaskGroups[0].Tasks[0].Copy()
	task3.Name = "follower2"
	task3.Driver = "mock_driver"
	task3.Config = map[string]interface{}{
		"run_for": "10s",
	}
	alloc.Job.TaskGroups[0].Tasks = append(alloc.Job.TaskGroups[0].Tasks, task2, task3)
	alloc.AllocatedResources.Tasks[task.Name] = tr
	alloc.AllocatedResources.Tasks[task2.Name] = tr
	alloc.AllocatedResources.Tasks[task3.Name] = tr

	conf, cleanup := testAllocRunnerConfig(t, alloc)
	defer cleanup()
	ar, err := NewAllocRunner(conf)
	require.NoError(t, err)
	defer ar.Destroy()
	go ar.Run()

	// Wait for tasks to start
	upd := conf.StateUpdater.(*MockStateUpdater)
	last := upd.Last()
	testutil.WaitForResult(func() (bool, error) {
		last = upd.Last()
		if last == nil {
			return false, fmt.Errorf("No updates")
		}
		if n := len(last.TaskStates); n != 3 {
			return false, fmt.Errorf("Not enough task states (want: 3; found %d)", n)
		}
		for name, state := range last.TaskStates {
			if state.State != structs.TaskStateRunning {
				return false, fmt.Errorf("Task %q is not running yet (it's %q)", name, state.State)
			}
		}
		return true, nil
	}, func(err error) {
		t.Fatalf("err: %v", err)
	})

	// Reset updates
	upd.Reset()

	// Stop alloc
	update := alloc.Copy()
	update.DesiredStatus = structs.AllocDesiredStatusStop
	ar.Update(update)

	// Wait for tasks to stop
	testutil.WaitForResult(func() (bool, error) {
		last := upd.Last()
		if last == nil {
			return false, fmt.Errorf("No updates")
		}
		if last.TaskStates["leader"].FinishedAt.UnixNano() >= last.TaskStates["follower1"].FinishedAt.UnixNano() {
			return false, fmt.Errorf("expected leader to finish before follower1: %s >= %s",
				last.TaskStates["leader"].FinishedAt, last.TaskStates["follower1"].FinishedAt)
		}
		if last.TaskStates["leader"].FinishedAt.UnixNano() >= last.TaskStates["follower2"].FinishedAt.UnixNano() {
			return false, fmt.Errorf("expected leader to finish before follower2: %s >= %s",
				last.TaskStates["leader"].FinishedAt, last.TaskStates["follower2"].FinishedAt)
		}
		return true, nil
	}, func(err error) {
		last := upd.Last()
		for name, state := range last.TaskStates {
			t.Logf("%s: %s", name, state.State)
		}
		t.Fatalf("err: %v", err)
	})
}

// TestAllocRunner_TaskLeader_StopRestoredTG asserts that when stopping a
// restored task group with a leader that failed before restoring the leader is
// not stopped as it does not exist.
// See https://github.com/hashicorp/nomad/issues/3420#issuecomment-341666932
func TestAllocRunner_TaskLeader_StopRestoredTG(t *testing.T) {
	t.Parallel()

	alloc := mock.Alloc()
	tr := alloc.AllocatedResources.Tasks[alloc.Job.TaskGroups[0].Tasks[0].Name]
	alloc.Job.TaskGroups[0].RestartPolicy.Attempts = 0

	// Create a leader and follower task in the task group
	task := alloc.Job.TaskGroups[0].Tasks[0]
	task.Name = "follower1"
	task.Driver = "mock_driver"
	task.KillTimeout = 10 * time.Second
	task.Config = map[string]interface{}{
		"run_for": "10s",
	}

	task2 := alloc.Job.TaskGroups[0].Tasks[0].Copy()
	task2.Name = "leader"
	task2.Driver = "mock_driver"
	task2.Leader = true
	task2.KillTimeout = 10 * time.Millisecond
	task2.Config = map[string]interface{}{
		"run_for": "10s",
	}

	alloc.Job.TaskGroups[0].Tasks = append(alloc.Job.TaskGroups[0].Tasks, task2)
	alloc.AllocatedResources.Tasks[task.Name] = tr
	alloc.AllocatedResources.Tasks[task2.Name] = tr

	conf, cleanup := testAllocRunnerConfig(t, alloc)
	defer cleanup()

	// Use a memory backed statedb
	conf.StateDB = state.NewMemDB()

	ar, err := NewAllocRunner(conf)
	require.NoError(t, err)

	// Mimic Nomad exiting before the leader stopping is able to stop other tasks.
	ar.tasks["leader"].UpdateState(structs.TaskStateDead, structs.NewTaskEvent(structs.TaskKilled))
	ar.tasks["follower1"].UpdateState(structs.TaskStateRunning, structs.NewTaskEvent(structs.TaskStarted))

	// Create a new AllocRunner to test RestoreState and Run
	ar2, err := NewAllocRunner(conf)
	require.NoError(t, err)
	defer ar2.Destroy()

	if err := ar2.Restore(); err != nil {
		t.Fatalf("error restoring state: %v", err)
	}
	ar2.Run()

	// Wait for tasks to be stopped because leader is dead
	testutil.WaitForResult(func() (bool, error) {
		alloc := ar2.Alloc()
		for task, state := range alloc.TaskStates {
			if state.State != structs.TaskStateDead {
				return false, fmt.Errorf("Task %q should be dead: %v", task, state.State)
			}
		}
		return true, nil
	}, func(err error) {
		t.Fatalf("err: %v", err)
	})

	// Make sure it GCs properly
	ar2.Destroy()

	select {
	case <-ar2.DestroyCh():
		// exited as expected
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for AR to GC")
	}
}

func TestAllocRunner_Update_Semantics(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	updatedAlloc := func(a *structs.Allocation) *structs.Allocation {
		upd := a.CopySkipJob()
		upd.AllocModifyIndex++

		return upd
	}

	alloc := mock.Alloc()
	alloc.Job.TaskGroups[0].Tasks[0].Driver = "mock_driver"
	conf, cleanup := testAllocRunnerConfig(t, alloc)
	defer cleanup()

	ar, err := NewAllocRunner(conf)
	require.NoError(err)

	upd1 := updatedAlloc(alloc)
	ar.Update(upd1)

	// Update was placed into a queue
	require.Len(ar.allocUpdatedCh, 1)

	upd2 := updatedAlloc(alloc)
	ar.Update(upd2)

	// Allocation was _replaced_

	require.Len(ar.allocUpdatedCh, 1)
	queuedAlloc := <-ar.allocUpdatedCh
	require.Equal(upd2, queuedAlloc)

	// Requeueing older alloc is skipped
	ar.Update(upd2)
	ar.Update(upd1)

	queuedAlloc = <-ar.allocUpdatedCh
	require.Equal(upd2, queuedAlloc)

	// Ignore after watch closed

	close(ar.waitCh)

	ar.Update(upd1)

	// Did not queue the update
	require.Len(ar.allocUpdatedCh, 0)
}

/*

import (
	"testing"

	"github.com/hashicorp/nomad/client/allocrunner/interfaces"
	clientconfig "github.com/hashicorp/nomad/client/config"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

func testAllocRunnerFromAlloc(t *testing.T, alloc *structs.Allocation) *allocRunner {
	cconf := clientconfig.DefaultConfig()
	config := &Config{
		ClientConfig: cconf,
		Logger:       testlog.HCLogger(t).With("unit_test", t.Name()),
		Alloc:        alloc,
	}

	ar := NewAllocRunner(config)
	return ar
}

func testAllocRunner(t *testing.T) *allocRunner {
	return testAllocRunnerFromAlloc(t, mock.Alloc())
}

// preRun is a test RunnerHook that captures whether Prerun was called on it
type preRun struct{ run bool }

func (p *preRun) Name() string { return "pre" }
func (p *preRun) Prerun() error {
	p.run = true
	return nil
}

// postRun is a test RunnerHook that captures whether Postrun was called on it
type postRun struct{ run bool }

func (p *postRun) Name() string { return "post" }
func (p *postRun) Postrun() error {
	p.run = true
	return nil
}

// Tests that prerun only runs pre run hooks.
func TestAllocRunner_Prerun_Basic(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	ar := testAllocRunner(t)

	// Overwrite the hooks with test hooks
	pre := &preRun{}
	post := &postRun{}
	ar.runnerHooks = []interfaces.RunnerHook{pre, post}

	// Run the hooks
	require.NoError(ar.prerun())

	// Assert only the pre is run
	require.True(pre.run)
	require.False(post.run)
}

// Tests that postrun only runs post run hooks.
func TestAllocRunner_Postrun_Basic(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	ar := testAllocRunner(t)

	// Overwrite the hooks with test hooks
	pre := &preRun{}
	post := &postRun{}
	ar.runnerHooks = []interfaces.RunnerHook{pre, post}

	// Run the hooks
	require.NoError(ar.postrun())

	// Assert only the pre is run
	require.True(post.run)
	require.False(pre.run)
}
*/
