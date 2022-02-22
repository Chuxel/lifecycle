package platform_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/apex/log"
	"github.com/apex/log/handlers/memory"
	"github.com/sclevine/spec"
	"github.com/sclevine/spec/report"

	"github.com/buildpacks/lifecycle"
	"github.com/buildpacks/lifecycle/api"
	"github.com/buildpacks/lifecycle/cmd/lifecycle/platform"
	h "github.com/buildpacks/lifecycle/testhelpers"
)

func TestAnalyzeInputs(t *testing.T) {
	for _, api := range api.Platform.Supported {
		spec.Run(t, "unit-analyzer/"+api.String(), testAnalyzeInputs(api.String()), spec.Parallel(), spec.Report(report.Terminal{}))
	}
}

func testAnalyzeInputs(platformAPI string) func(t *testing.T, when spec.G, it spec.S) {
	return func(t *testing.T, when spec.G, it spec.S) {
		var (
			av         *platform.AnalyzeInputsValidator
			logHandler *memory.Handler
			logger     lifecycle.Logger
		)
		it.Before(func() {
			av = &platform.AnalyzeInputsValidator{PlatformAPI: api.MustParse(platformAPI)}
			logHandler = memory.New()
			logger = &log.Logger{Handler: logHandler}
		})

		when("called without an app image", func() {
			it("errors", func() {
				_, err := av.Resolve(platform.AnalyzeInputs{}, []string{}, logger)
				h.AssertNotNil(t, err)
				expected := "failed to parse arguments: received 0 arguments, but expected 1"
				h.AssertStringContains(t, err.Error(), expected)
			})
		})

		when("platform api >= 0.7", func() {
			it.Before(func() {
				h.SkipIf(t, api.MustParse(platformAPI).LessThan("0.7"), "")
			})

			when("run image", func() {
				when("not provided", func() {
					it("falls back to stack.toml", func() {
						inputs := platform.AnalyzeInputs{
							StackPath: filepath.Join("testdata", "layers", "stack.toml"),
						}
						ret, err := av.Resolve(inputs, []string{"some-image"}, logger)
						h.AssertNil(t, err)
						h.AssertEq(t, ret.RunImageRef, "some-run-image")
					})

					when("stack.toml not present", func() {
						it("errors", func() {
							inputs := platform.AnalyzeInputs{
								StackPath: "not-exist-stack.toml",
							}
							_, err := av.Resolve(inputs, []string{"some-image"}, logger)
							h.AssertNotNil(t, err)
							expected := "-run-image is required when there is no stack metadata available"
							h.AssertStringContains(t, err.Error(), expected)
						})
					})
				})
			})
		})

		when("platform api < 0.7", func() {
			it.Before(func() {
				h.SkipIf(t, api.MustParse(platformAPI).AtLeast("0.7"), "")
			})

			when("cache image tag and cache directory are both blank", func() {
				it("warns", func() {
					_, err := av.Resolve(platform.AnalyzeInputs{}, []string{"some-image"}, logger)
					h.AssertNil(t, err)
					expected := "Not restoring cached layer metadata, no cache flag specified."
					assertLogEntry(t, logHandler, expected)
				})
			})

			when("run image", func() {
				when("not provided", func() {
					it("does not warn", func() {
						inputs := platform.AnalyzeInputs{
							StackPath: "not-exist-stack.toml",
						}
						_, err := av.Resolve(inputs, []string{"some-image"}, logger)
						h.AssertNil(t, err)
						assertLogEntryNotContains(t, logHandler, `no stack metadata found at path ''`)
						assertLogEntryNotContains(t, logHandler, `Previous image with name "" not found`)
					})
				})
			})

			when("layers path is provided", func() {
				it("uses the group path at the layers path and writes analyzed.toml at the layers path", func() {
					h.SkipIf(t,
						api.MustParse(platformAPI).LessThan("0.5"),
						"Platform API < 0.5 reads and writes to the working directory",
					)

					inputs := platform.AnalyzeInputs{
						AnalyzedPath:    platform.PlaceholderAnalyzedPath,
						LegacyGroupPath: platform.PlaceholderGroupPath,
						LayersDir:       filepath.Join("testdata", "other-layers"),
					}
					ret, err := av.Resolve(inputs, []string{"some-image"}, logger)
					h.AssertNil(t, err)
					h.AssertEq(t, ret.LegacyGroupPath, filepath.Join("testdata", "other-layers", "group.toml"))
					h.AssertEq(t, ret.AnalyzedPath, filepath.Join("testdata", "other-layers", "analyzed.toml"))
				})
			})
		})

		when("platform api < 0.5", func() {
			it.Before(func() {
				h.SkipIf(t, api.MustParse(platformAPI).AtLeast("0.6"), "")
			})

			when("layers path is provided", func() {
				it("uses the group path at the working directory and writes analyzed.toml at the working directory", func() {
					inputs := platform.AnalyzeInputs{
						AnalyzedPath:    filepath.Join(".", "analyzed.toml"),
						LegacyGroupPath: filepath.Join(".", "group.toml"),
						LayersDir:       filepath.Join("testdata", "other-layers"),
					}
					ret, err := av.Resolve(inputs, []string{"some-image"}, logger)
					h.AssertNil(t, err)
					h.AssertEq(t, ret.LegacyGroupPath, filepath.Join(".", "group.toml"))
					h.AssertEq(t, ret.AnalyzedPath, filepath.Join(".", "analyzed.toml"))
				})
			})
		})
	}
}

// TODO: put in some common place
func assertLogEntry(t *testing.T, logHandler *memory.Handler, expected string) {
	t.Helper()
	var messages []string
	for _, le := range logHandler.Entries {
		messages = append(messages, le.Message)
		if strings.Contains(le.Message, expected) {
			return
		}
	}
	t.Fatalf("Expected log entries %+v to contain %s", messages, expected)
}

// TODO: put in some common place
func assertLogEntryNotContains(t *testing.T, logHandler *memory.Handler, expected string) {
	t.Helper()
	var messages []string
	for _, le := range logHandler.Entries {
		messages = append(messages, le.Message)
		if strings.Contains(le.Message, expected) {
			fmtMessage := "\n" + strings.Join(messages, "\n") + "\n"
			t.Fatalf("Expected log entries: %s not to contain \n'%s'", fmtMessage, expected)
		}
	}
}