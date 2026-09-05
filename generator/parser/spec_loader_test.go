// Copyright 2026, Jamf Software LLC

package parser

import (
	"path/filepath"
	"testing"
)

// The whole reason ParseSpec shares one loader is that openapi3.Loader caches a
// document by the path it was read from. 126 of the 165 committed specs
// external-$ref specs/_MonolithLibrary.yaml, 100 KB of YAML, so the cache is
// what turns 126 unmarshals of that file into one — it took the suite from 72s
// to 31s and make generate with it.
//
// Nothing about that is part of kin-openapi's documented contract. If a future
// version stops caching, or caches under a different key, every spec is
// unmarshalled again and the win silently evaporates: no test fails, no output
// changes, the generator is simply slow again. That is the failure mode this
// test exists to convert into a loud one. It asserts the mechanism, not a
// duration, because a duration assertion on a shared runner is a flake.
func TestSharedSpecLoaderCachesByPath(t *testing.T) {
	lib := filepath.Join("..", "..", "specs", "_MonolithLibrary.yaml")

	specLoaderMu.Lock()
	first, err := specLoader.LoadFromFile(lib)
	specLoaderMu.Unlock()
	if err != nil {
		t.Fatalf("loading %s: %v", lib, err)
	}

	specLoaderMu.Lock()
	second, err := specLoader.LoadFromFile(lib)
	specLoaderMu.Unlock()
	if err != nil {
		t.Fatalf("re-loading %s: %v", lib, err)
	}

	if first != second {
		t.Error("openapi3.Loader returned a different document for the same path — " +
			"it no longer caches by path, so ParseSpec's shared loader re-unmarshals " +
			"_MonolithLibrary.yaml once per referring spec. The suite and make generate " +
			"are now roughly 2x slower. Either restore caching in the loader or memoize " +
			"the library document in ParseSpec.")
	}
}
