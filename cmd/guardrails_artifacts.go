package cmd

// guardrailsPinnedVersion is the Guardrails release installed by this version
// of Konvu CLI.
const guardrailsPinnedVersion = "v0.5.1"

type guardrailsArtifact struct {
	archiveSHA256         string
	mainSHA256            string
	resourceScannerSHA256 string
}

// guardrailsArtifacts binds the proprietary runtime bytes to this public CLI
// release. Remote checksums are intentionally not a trust anchor: an attacker
// able to replace a CDN archive could otherwise replace its checksum too.
var guardrailsArtifacts = map[string]guardrailsArtifact{
	"aarch64-apple-darwin": {
		archiveSHA256:         "1d7e495805a4eee04b39b211d5bf6a9775627c190f03fc04cc7b5c64855bea2e",
		mainSHA256:            "795f7855051eb1d5a5ae5b8eebe5df8ce9ba8d7767e3222f337fcba63411f360",
		resourceScannerSHA256: "c029b23314acb3a8cebd06c79dbdec9c0934d56a06c13d784aa8b52b25bcddf0",
	},
	"x86_64-apple-darwin": {
		archiveSHA256:         "9c69789f4f006b43bf75fd0eb9ea135bd1ebddf0256f5cd6db92facb1787be54",
		mainSHA256:            "48b1f3de347a7eb58f1757f66f969c1d9587db6caacb0f7ba3e883450dd69e97",
		resourceScannerSHA256: "8e2e16fdc1f761ccf674276328e9c5020289b12ed76d62ab325066d6ca02b8a0",
	},
	"aarch64-unknown-linux-gnu": {
		archiveSHA256:         "b9f55af045b22d8430d70f496754f263e7f75770ae100ae60346ee398e0400b2",
		mainSHA256:            "0a60ee8d6d955e67a7634300af7525e0a9b6fafe510b90d283448f5304abffaa",
		resourceScannerSHA256: "9f6a311a68e8854c3572ec40842a069e6465b68db88fa8cb22d7bf2e4f5aede6",
	},
	"x86_64-unknown-linux-gnu": {
		archiveSHA256:         "2586cece415a092c15304d74537420decf650ef20134eac90cec874152dd9f3f",
		mainSHA256:            "4fa6f40442ea80e35ac6c0a5420be8859850cf77d125efbed8ea74a8aff859ba",
		resourceScannerSHA256: "4804b8d8544ac6d557b7e69fd5c9ee4e99e6cbcb0b0c100863b7e9f91e8f2a3a",
	},
}
