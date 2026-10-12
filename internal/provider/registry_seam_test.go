package provider

// This file (a _test.go file, so it is compiled only for tests) provides a seam
// that registers and clears provider constructors. It lets in-package tests
// register a deterministic, network-free stub provider while keeping production
// providers limited to the registry entries declared in provider.go.
//
// The seam mutates the package registry maps, so tests that use it must not run
// concurrently with each other or with any code that reads the registry. The
// in-package tests here are sequential (no t.Parallel with the seam held), so it
// is race-safe under `go test -race`.

// registerEmbedderForTest registers an embed constructor under name and returns
// a function that restores the previous registration (removing it when there
// was none). It is intended for in-package tests only.
func registerEmbedderForTest(name string, constructor EmbedderConstructor) func() {
	previous, had := embedders[name]

	embedders[name] = constructor

	return func() {
		if had {
			embedders[name] = previous

			return
		}

		delete(embedders, name)
	}
}

// registerGeneratorForTest registers a generator constructor under name and
// returns a function that restores the previous registration (removing it when
// there was none). It is intended for in-package tests only.
func registerGeneratorForTest(name string, constructor GeneratorConstructor) func() {
	previous, had := generators[name]

	generators[name] = constructor

	return func() {
		if had {
			generators[name] = previous

			return
		}

		delete(generators, name)
	}
}
