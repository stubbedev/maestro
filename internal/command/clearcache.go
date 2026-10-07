// Ports src/Composer/Command/ClearCacheCommand.php.

package command

import (
	"os"
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	"github.com/stubbedev/maestro/internal/store"
)

func init() {
	registerCommand(OrderClearCache, func() console.Commander { return NewClearCacheCommand() })
}

// ClearCacheCommand is Composer\Command\ClearCacheCommand.
//
// maestro's package store (docs/PORTING.md deviation 1) holds the extracted
// form of the files cache, so it follows cache-files-dir: a full clear
// empties it and --gc prunes what was unused for cache-files-ttl. Both are
// silent, so the output stays Composer's. Likewise the decoded repository
// metadata (deviation 3) follows cache-repo-dir: a full clear removes it
// and --gc removes what was not written for cache-ttl.
type ClearCacheCommand struct{ *BaseCommand }

// NewClearCacheCommand ports new ClearCacheCommand().
func NewClearCacheCommand() *ClearCacheCommand {
	c := &ClearCacheCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("clear-cache").
		SetAliases("clearcache", "cc").
		SetDescription("Clears composer's internal package cache").
		SetDefinitionItems(
			console.MustOption("gc", "", console.OptionValueNone, "Only run garbage collection, not a full cache clear", nil),
		).
		SetHelp(`The <info>clear-cache</info> deletes all cached packages from composer's
cache directory.

Read more at https://getcomposer.org/doc/03-cli.md#clear-cache-clearcache-cc`)

	return c
}

// PHPClass implements php.Classer.
func (*ClearCacheCommand) PHPClass() string { return `Composer\Command\ClearCacheCommand` }

// Execute implements console.Executor.
func (c *ClearCacheCommand) Execute(in console.Input, _ console.Output) (int, error) {
	composer, err := c.TryComposer(nil, nil)
	if err != nil {
		return 0, err
	}
	var cfg *config.Config
	if composer != nil {
		cfg = composer.Config()
	} else if cfg, err = c.factory().CreateConfig(nil, ""); err != nil {
		return 0, err
	}

	out := c.IO()
	gc := console.BoolOption(in, "gc")

	get := func(key string) (any, error) { return cfg.Get(key, 0) }

	for _, key := range []string{"cache-vcs-dir", "cache-repo-dir", "cache-files-dir", "cache-dir"} {
		v, err := get(key)
		if err != nil {
			return 0, err
		}
		// only individual dirs get garbage collected
		if key == "cache-dir" && gc {
			continue
		}

		cachePath, ok := php.Realpath(php.ToString(v))
		if !ok || !php.Truthy(cachePath) {
			// realpath()'s false prints as an empty string
			out.WriteError("<info>Cache directory does not exist ("+key+"): </info>", true, io.Normal)

			continue
		}
		ch, err := cache.New(out, cachePath, "", nil, false)
		if err != nil {
			return 0, err
		}
		readOnly, err := get("cache-read-only")
		if err != nil {
			return 0, err
		}
		ch.SetReadOnly(php.ToBool(readOnly))
		if !ch.IsEnabled() {
			out.WriteError("<info>Cache is not enabled ("+key+"): "+cachePath+"</info>", true, io.Normal)

			continue
		}

		if gc {
			out.WriteError("<info>Garbage-collecting cache ("+key+"): "+cachePath+"</info>", true, io.Normal)
			switch key {
			case "cache-files-dir":
				ttl, err := get("cache-files-ttl")
				if err != nil {
					return 0, err
				}
				maxSize, err := get("cache-files-maxsize")
				if err != nil {
					return 0, err
				}
				if _, err := ch.Gc(php.ToNativeInt(ttl), php.ToInt(maxSize)); err != nil {
					return 0, err
				}
				pruneStore(time.Duration(php.ToInt(ttl)) * time.Second)
			case "cache-repo-dir":
				ttl, err := get("cache-ttl")
				if err != nil {
					return 0, err
				}
				collected, err := ch.Gc(php.ToNativeInt(ttl), 1024*1024*1024 /* 1GB, this should almost never clear anything that is not outdated */)
				if err != nil {
					return 0, err
				}
				if collected {
					_ = composerrepo.GcDecodedCache(cache.DecodedMetadata(), php.ToNativeInt(ttl))
				}
			case "cache-vcs-dir":
				ttl, err := get("cache-ttl")
				if err != nil {
					return 0, err
				}
				if _, err := ch.GcVcsCache(php.ToNativeInt(ttl)); err != nil {
					return 0, err
				}
			}
		} else {
			out.WriteError("<info>Clearing cache ("+key+"): "+cachePath+"</info>", true, io.Normal)
			cleared, err := ch.Clear()
			if err != nil {
				return 0, err
			}
			switch {
			case key == "cache-files-dir":
				pruneStore(0)
			case key == "cache-repo-dir" && cleared:
				_ = composerrepo.ClearDecodedCache(cache.DecodedMetadata())
			}
		}
	}

	if gc {
		out.WriteError("<info>All caches garbage-collected.</info>", true, io.Normal)
	} else {
		out.WriteError("<info>All caches cleared.</info>", true, io.Normal)
	}

	return 0, nil
}

// pruneStore removes the store's releases unused for maxAge (all of them
// for 0), when there is a store.
func pruneStore(maxAge time.Duration) {
	root := cache.Store()
	if _, err := os.Stat(root); err != nil {
		return
	}
	s, err := store.Open(root, nil)
	if err != nil {
		return
	}
	_, _ = s.Prune(maxAge)
}
