package localinlinecache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/containerd/containerd/v2/core/content"
	c8dimages "github.com/containerd/containerd/v2/core/images"
	"github.com/containerd/containerd/v2/core/remotes/docker"
	"github.com/distribution/reference"
	"github.com/moby/buildkit/cache/remotecache"
	registryremotecache "github.com/moby/buildkit/cache/remotecache/registry"
	v1 "github.com/moby/buildkit/cache/remotecache/v1"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/solver"
	"github.com/moby/buildkit/worker"
	imagestore "github.com/moby/moby/v2/daemon/internal/image"
	refstore "github.com/moby/moby/v2/daemon/internal/refstore"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// ResolveCacheImporterFunc returns a resolver function for local inline cache
func ResolveCacheImporterFunc(sm *session.Manager, resolverFunc docker.RegistryHosts, cs content.Store, rs refstore.Store, is imagestore.Store) remotecache.ResolveCacheImporterFunc {
	upstream := registryremotecache.ResolveCacheImporterFunc(sm, cs, resolverFunc)

	return func(ctx context.Context, group session.Group, attrs map[string]string) (remotecache.Importer, ocispec.Descriptor, error) {
		if dt, err := tryImportLocal(rs, is, attrs["ref"]); err == nil {
			return newLocalImporter(dt), ocispec.Descriptor{}, nil
		}
		return upstream(ctx, group, attrs)
	}
}

func tryImportLocal(rs refstore.Store, is imagestore.Store, refStr string) ([]byte, error) {
	ref, err := reference.ParseNormalizedNamed(refStr)
	if err != nil {
		return nil, err
	}
	dgst, err := rs.Get(ref)
	if err != nil {
		return nil, err
	}
	img, err := is.Get(imagestore.ID(dgst))
	if err != nil {
		return nil, err
	}

	return img.RawJSON(), nil
}

func newLocalImporter(dt []byte) remotecache.Importer {
	return &localImporter{dt: dt}
}

type localImporter struct {
	dt []byte
}

func (li *localImporter) Resolve(ctx context.Context, _ ocispec.Descriptor, id string, w worker.Worker) (solver.CacheManager, error) {
	cc := v1.NewCacheChains()
	if err := li.importInlineCache(ctx, li.dt, cc); err != nil {
		return nil, err
	}

	keysStorage, resultStorage, err := v1.NewCacheKeyStorage(cc, w)
	if err != nil {
		return nil, err
	}
	return solver.NewCacheManager(ctx, id, keysStorage, resultStorage), nil
}

func (li *localImporter) importInlineCache(ctx context.Context, dt []byte, cc solver.CacheExporterTarget) error {
	var img image

	if err := json.Unmarshal(dt, &img); err != nil {
		return err
	}

	if img.Cache == nil {
		return nil
	}

	var config v1.CacheConfig
	if err := json.Unmarshal(img.Cache, &config.Records); err != nil {
		return err
	}

	createdDates, createdMsg, err := parseCreatedLayerInfo(img)
	if err != nil {
		return err
	}

	layers := v1.DescriptorProvider{}
	for i, diffID := range img.Rootfs.DiffIDs {
		dgst := digest.Digest(diffID.String())
		desc := ocispec.Descriptor{
			Digest:      dgst,
			Size:        -1,
			MediaType:   c8dimages.MediaTypeDockerSchema2Layer,
			Annotations: map[string]string{},
		}
		if createdAt := createdDates[i]; createdAt != "" {
			desc.Annotations["buildkit/createdat"] = createdAt
		}
		if createdBy := createdMsg[i]; createdBy != "" {
			desc.Annotations["buildkit/description"] = createdBy
		}
		desc.Annotations["containerd.io/uncompressed"] = img.Rootfs.DiffIDs[i].String()
		layers[dgst] = v1.DescriptorProviderPair{
			Descriptor: desc,
			Provider:   &emptyProvider{},
		}
		config.Layers = append(config.Layers, v1.CacheLayer{
			Blob:        dgst,
			ParentIndex: i - 1,
		})
	}

	return v1.ParseConfig(config, layers, cc)
}

type image struct {
	Rootfs struct {
		DiffIDs []digest.Digest `json:"diff_ids"`
	} `json:"rootfs"`
	Cache   []byte `json:"moby.buildkit.cache.v0"`
	History []struct {
		Created    *time.Time `json:"created,omitempty"`
		CreatedBy  string     `json:"created_by,omitempty"`
		EmptyLayer bool       `json:"empty_layer,omitempty"`
	} `json:"history,omitempty"`
}

func parseCreatedLayerInfo(img image) ([]string, []string, error) {
	dates := make([]string, 0, len(img.Rootfs.DiffIDs))
	createdBy := make([]string, 0, len(img.Rootfs.DiffIDs))
	for _, h := range img.History {
		if !h.EmptyLayer {
			str := ""
			if h.Created != nil {
				dt, err := h.Created.MarshalText()
				if err != nil {
					return nil, nil, err
				}
				str = string(dt)
			}
			dates = append(dates, str)
			createdBy = append(createdBy, h.CreatedBy)
		}
	}
	return dates, createdBy, nil
}

type emptyProvider struct{}

func (p *emptyProvider) ReaderAt(ctx context.Context, dec ocispec.Descriptor) (content.ReaderAt, error) {
	return nil, errors.Errorf("ReaderAt not implemented for empty provider")
}
