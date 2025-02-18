package registry

import (
	"context"
	"encoding/json"

	"github.com/moby/buildkit/cache/remotecache"
	v1 "github.com/moby/buildkit/cache/remotecache/v1"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/solver"
	digest "github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
)

func ResolveCacheExporterFunc() remotecache.ResolveCacheExporterFunc {
	return func(ctx context.Context, _ session.Group, _ map[string]string) (remotecache.Exporter, error) {
		return NewExporter(), nil
	}
}

func NewExporter() remotecache.Exporter {
	cc := v1.NewCacheChains()
	return &exporter{CacheExporterTarget: cc, chains: cc}
}

type exporter struct {
	solver.CacheExporterTarget
	chains *v1.CacheChains
}

func (ce *exporter) Finalize(ctx context.Context) (map[string]string, error) {
	return nil, nil
}

func (ce *exporter) reset() {
	cc := v1.NewCacheChains()
	ce.CacheExporterTarget = cc
	ce.chains = cc
}

func (ce *exporter) ExportForLayers(ctx context.Context, layers []digest.Digest) ([]byte, error) {
	config, descs, err := ce.chains.Marshal(ctx)
	if err != nil {
		return nil, err
	}

	descs2 := map[digest.Digest]v1.DescriptorProviderPair{}
	for _, k := range layers {
		if v, ok := descs[k]; ok {
			descs2[k] = v
			continue
		}
		// fallback for uncompressed digests
		for _, v := range descs {
			if uc := v.Descriptor.Annotations["containerd.io/uncompressed"]; uc == string(k) {
				descs2[v.Descriptor.Digest] = v
			}
		}
	}

	cc := v1.NewCacheChains()
	if err := v1.ParseConfig(*config, descs2, cc); err != nil {
		return nil, err
	}

	cfg, _, err := cc.Marshal(ctx)
	if err != nil {
		return nil, err
	}

	if len(cfg.Layers) == 0 {
		logrus.Warn("failed to match any cache with layers")
		return nil, nil
	}

	cache := map[int]int{}

	// reorder layers based on the order in the image
	for i, r := range cfg.Records {
		for j, rr := range r.Results {
			n := getSortedLayerIndex(rr.LayerIndex, cfg.Layers, cache)
			rr.LayerIndex = n
			r.Results[j] = rr
			cfg.Records[i] = r
		}
	}

	dt, err := json.Marshal(cfg.Records)
	if err != nil {
		return nil, err
	}
	ce.reset()

	return dt, nil
}

func getSortedLayerIndex(idx int, layers []v1.CacheLayer, cache map[int]int) int {
	if idx == -1 {
		return -1
	}
	l := layers[idx]
	if i, ok := cache[idx]; ok {
		return i
	}
	cache[idx] = getSortedLayerIndex(l.ParentIndex, layers, cache) + 1
	return cache[idx]
}
