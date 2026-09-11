### Legacy-to-DDM Payload Conversion

When `import-profile` processes a mobileconfig, compatible legacy payloads auto-convert to native DDM blueprint components instead of wrapping in `com.jamf.ddm-configuration-profile`. `--legacy` skips all conversion. Unsupported payload types filtered by default; `--include-unsupported` overrides.

Converters live in `internal/profileconvert/ddm_*.go`. Registry orchestration in `ddm_converter.go` (`ConvertToDDMComponents()`); converters register in `init()`. Current: passcode, safari, software-update deferrals, RSR, SoftwareUpdate profile. Multiple converters targeting the same component ID (e.g. deferrals + RSR + SoftwareUpdate → `software-update-settings`) deep-merge; orchestrator backfills missing scaffold sections.

Partial converters return `remaining` keys (extracted from shared payload types like `applicationaccess`); full converters return nil `remaining`. Components requiring complex schemas read base config from `blueprintcomponents.Scaffolds` at runtime, `clearIncluded()`, then overlay converted keys with `Included: true`. Jamf UI requires every section present — omitting sections blanks the panel.

Adding a converter: create `ddm_<name>.go`, implement `convertFunc`, register via `newXxxConverter()` in `ddm_converter.go` `init()`, add tests in `ddm_converter_test.go`.

