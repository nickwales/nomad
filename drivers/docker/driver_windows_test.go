// +build windows

package docker

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/lib/freeport"
	"github.com/hashicorp/nomad/client/testutil"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/plugins/drivers"
	tu "github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

var (
	// busyboxImageID is the image that should be ran
	busyboxImageID = "dantoml/busybox-windows:08012019"
)

// Returns a task with a reserved and dynamic port. The ports are returned
// respectively.
func dockerTask(t *testing.T) (*drivers.TaskConfig, *TaskConfig, []int) {
	ports := freeport.GetT(t, 2)
	dockerReserved := ports[0]
	dockerDynamic := ports[1]

	cfg := TaskConfig{
		Image:   busyboxImageID,
		Command: busyboxLongRunningCmd[0],
		Args:    busyboxLongRunningCmd[1:],
	}
	task := &drivers.TaskConfig{
		ID:   uuid.Generate(),
		Name: "redis-demo",
		Resources: &drivers.Resources{
			NomadResources: &structs.AllocatedTaskResources{
				Memory: structs.AllocatedMemoryResources{
					MemoryMB: 256,
				},
				Cpu: structs.AllocatedCpuResources{
					CpuShares: 512,
				},
				Networks: []*structs.NetworkResource{
					{
						IP:            "127.0.0.1",
						ReservedPorts: []structs.Port{{Label: "main", Value: dockerReserved}},
						DynamicPorts:  []structs.Port{{Label: "REDIS", Value: dockerDynamic}},
					},
				},
			},
			LinuxResources: &drivers.LinuxResources{
				CPUShares:        512,
				MemoryLimitBytes: 256 * 1024 * 1024,
			},
		},
	}

	require.NoError(t, task.EncodeConcreteDriverConfig(&cfg))

	return task, &cfg, ports
}

func TestDockerDriver_Entrypoint(t *testing.T) {
	if !tu.IsTravis() {
		t.Parallel()
	}
	if !testutil.DockerIsConnected(t) {
		t.Skip("Docker not connected")
	}

	entrypoint := []string{"cmd.exe", "/s", "/c"}
	task, cfg, _ := dockerTask(t)
	cfg.Entrypoint = entrypoint
	cfg.Command = strings.Join(busyboxLongRunningCmd, " ")
	cfg.Args = []string{}

	require.NoError(t, task.EncodeConcreteDriverConfig(cfg))

	client, driver, handle, cleanup := dockerSetup(t, task)
	defer cleanup()

	require.NoError(t, driver.WaitUntilStarted(task.ID, 5*time.Second))

	container, err := client.InspectContainer(handle.containerID)
	require.NoError(t, err)

	require.Len(t, container.Config.Entrypoint, 3, "Expected one entrypoint")
	require.Equal(t, entrypoint, container.Config.Entrypoint, "Incorrect entrypoint ")
}
