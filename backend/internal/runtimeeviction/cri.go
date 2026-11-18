package runtimeeviction

import (
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"net"
)

type RuntimeClient struct {
	conn   *grpc.ClientConn
	client runtimeapi.RuntimeServiceClient
}

func DialRuntime(ctx context.Context, socket string) (*RuntimeClient, error) {
	conn, e := grpc.NewClient("passthrough:///cri", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}))
	if e != nil {
		return nil, e
	}
	return &RuntimeClient{conn: conn, client: runtimeapi.NewRuntimeServiceClient(conn)}, nil
}
func (r *RuntimeClient) Close() error { return r.conn.Close() }
func (r *RuntimeClient) StopContainer(ctx context.Context, id string) error {
	_, e := r.client.StopContainer(ctx, &runtimeapi.StopContainerRequest{ContainerId: id, Timeout: 10})
	return e
}
func (r *RuntimeClient) ListContainers(ctx context.Context) ([]Container, error) {
	list, e := r.client.ListContainers(ctx, &runtimeapi.ListContainersRequest{})
	if e != nil {
		return nil, e
	}
	out := []Container{}
	for _, item := range list.Containers {
		status, e := r.client.ContainerStatus(ctx, &runtimeapi.ContainerStatusRequest{ContainerId: item.Id})
		if e != nil {
			return nil, e
		}
		sandbox, e := r.client.PodSandboxStatus(ctx, &runtimeapi.PodSandboxStatusRequest{PodSandboxId: item.PodSandboxId})
		if e != nil {
			return nil, e
		}
		if status.Status == nil || status.Status.Metadata == nil || sandbox.Status == nil || sandbox.Status.Metadata == nil {
			return nil, fmt.Errorf("CRI identity unavailable")
		}
		m := sandbox.Status.Metadata
		if status.Status.Id != item.Id || m.Uid == "" || m.Namespace == "" || m.Name == "" || status.Status.Metadata.Name == "" {
			return nil, fmt.Errorf("CRI returned incomplete container or sandbox identity")
		}
		state := "unknown"
		switch status.Status.State {
		case runtimeapi.ContainerState_CONTAINER_RUNNING:
			state = "running"
		case runtimeapi.ContainerState_CONTAINER_CREATED:
			state = "created"
		case runtimeapi.ContainerState_CONTAINER_EXITED:
			state = "exited"
		}
		out = append(out, Container{ID: status.Status.Id, PodUID: m.Uid, Name: status.Status.Metadata.Name, Namespace: m.Namespace, PodName: m.Name, State: state})
	}
	return out, nil
}
