package docker

import (
	"errors"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/errdefs"

	"github.com/RoundpenAI/roundpen/internal/backend"
)

func TestIsNameConflict(t *testing.T) {
	if isNameConflict(nil) {
		t.Fatal("nil")
	}
	if !isNameConflict(errdefs.Conflict(errors.New("Conflict. The container name \"/roundpen-admin\" is already in use"))) {
		t.Fatal("errdefs conflict")
	}
	if !isNameConflict(errors.New(`Error response from daemon: Conflict. The container name "/roundpen-admin" is already in use`)) {
		t.Fatal("string conflict")
	}
	if isNameConflict(errors.New("image not found")) {
		t.Fatal("unrelated")
	}
}

func TestCanReuseContainer(t *testing.T) {
	const image = "ghcr.io/roundpenai/code-agent:0.1.0"
	base := types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{ID: "abc"},
		Config: &container.Config{
			Image:  image,
			Labels: map[string]string{"roundpen.sandbox_id": "admin"},
		},
		Mounts: []types.MountPoint{{
			Type:        mount.TypeBind,
			Source:      "/data/sandboxes/user-admin/workspace",
			Destination: "/workspace",
		}},
	}
	opts := backend.CreateOpts{
		SandboxID: "admin",
		Image:     image,
		MountDir:  "/data/sandboxes/user-admin/workspace",
	}
	if !canReuseContainer(base, opts) {
		t.Fatal("same id+image+mount should reuse")
	}
	opts.MountDir = "/other/path"
	if canReuseContainer(base, opts) {
		t.Fatal("mount mismatch must not reuse")
	}
	opts.MountDir = "/data/sandboxes/user-admin/workspace"
	opts.SandboxID = "other"
	if canReuseContainer(base, opts) {
		t.Fatal("wrong sandbox id must not reuse")
	}
	opts.SandboxID = "admin"
	opts.Image = "ghcr.io/roundpenai/code-agent:0.2.0"
	if canReuseContainer(base, opts) {
		t.Fatal("image mismatch must not reuse")
	}
}
