package container

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"gotest.tools/v3/assert"
)

// TestContainerConfig holds container configuration struct that
// are used in api calls.
type TestContainerConfig struct {
	Name             string
	Config           *container.Config
	HostConfig       *container.HostConfig
	NetworkingConfig *network.NetworkingConfig
	Platform         *ocispec.Platform
}

// create creates a container with the specified options
func create(ctx context.Context, t *testing.T, client client.APIClient, ops ...func(*TestContainerConfig)) (container.CreateResponse, error) {
	t.Helper()
	cmd := []string{"top"}
	if runtime.GOOS == "windows" {
		cmd = []string{"sleep", "240"}
	}
	config := &TestContainerConfig{
		Config: &container.Config{
			Image: "busybox",
			Cmd:   cmd,
		},
		HostConfig:       &container.HostConfig{},
		NetworkingConfig: &network.NetworkingConfig{},
	}

	for _, op := range ops {
		op(config)
	}

	return client.ContainerCreate(ctx, config.Config, config.HostConfig, config.NetworkingConfig, config.Platform, config.Name)
}

// Create creates a container with the specified options, asserting that there was no error
func Create(ctx context.Context, t *testing.T, client client.APIClient, ops ...func(*TestContainerConfig)) string {
	t.Helper()
	c, err := create(ctx, t, client, ops...)
	assert.NilError(t, err)

	return c.ID
}

// CreateExpectingErr creates a container, expecting an error with the specified message
func CreateExpectingErr(ctx context.Context, t *testing.T, client client.APIClient, errMsg string, ops ...func(*TestContainerConfig)) {
	_, err := create(ctx, t, client, ops...)
	assert.ErrorContains(t, err, errMsg)
}

// Run creates and start a container with the specified options
func Run(ctx context.Context, t *testing.T, client client.APIClient, ops ...func(*TestContainerConfig)) string {
	t.Helper()
	id := Create(ctx, t, client, ops...)

	err := client.ContainerStart(ctx, id, types.ContainerStartOptions{})
	assert.NilError(t, err)

	return id
}

type streams struct {
	stdout, stderr bytes.Buffer
}

// demultiplexStreams starts a goroutine to demultiplex stdout and stderr from the types.HijackedResponse resp and
// waits until either multiplexed stream reaches EOF or the context expires. It unconditionally closes resp and waits
// until the demultiplexing goroutine has finished its work before returning.
func demultiplexStreams(ctx context.Context, resp types.HijackedResponse) (streams, error) {
	var s streams
	outputDone := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		_, err := stdcopy.StdCopy(&s.stdout, &s.stderr, resp.Reader)
		outputDone <- err
		wg.Done()
	}()

	var err error
	select {
	case copyErr := <-outputDone:
		err = copyErr
		break
	case <-ctx.Done():
		err = ctx.Err()
	}

	resp.Close()
	wg.Wait()
	return s, err
}
