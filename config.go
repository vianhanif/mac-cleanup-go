package main

import "time"

// Progress display
const (
	progressUpdateFreq = 80 * time.Millisecond
	progressLabelWidth = 40
)

// Summary table
const (
	truncErrMsgLen = 60
)

// Safe path allowlist - ONLY these paths may ever be passed to safeDelete().
var allowedPathSuffixes = []string{
	"Library/Caches/",
	"Library/Logs/",
	"Library/Developer/Xcode/DerivedData/",
	"Library/Developer/Xcode/Archives/",
	"Library/Developer/Xcode/iOS DeviceSupport/",
}

// Hard-blocked paths - safeDelete() refuses any path with these prefixes.
// Protects boot performance, system stability, and browser sessions.
var neverTouchPaths = []string{
	"Library/Application Support/",
	"Library/LaunchAgents/",
	"Library/StartupItems/",
	"Library/Fonts/",
	"/System/",
	"/usr/",
	"/bin/",
	"/sbin/",
	"/etc/",
	"/var/",
	"/private/",
	"/Library/LaunchDaemons/",
	"/Library/Extensions/",
}

// Cache exclusions - subdirectory names under ~/Library/Caches/ never deleted.
var cacheExclusions = []string{
	"com.apple.Safari",
	"com.apple.Spotlight",
	"com.apple.metadata",
	"com.apple.bird",
	"com.apple.akd",
	"com.apple.GSSFramework",
	"CloudKit",
	"com.apple.iCloudDrive",
	"com.google.Chrome",
	"com.google.Chrome.canary",
	"org.mozilla.firefox",
	"org.mozilla.nightly",
	"com.microsoft.edgemac",
	"com.microsoft.edgemac.Beta",
	"com.brave.Browser",
	"com.brave.Browser.beta",
	"com.operasoftware.Opera",
	"com.vivaldi.Vivaldi",
	"com.apple.SafariTechnologyPreview",
	"com.tinyspeck.slackmacgap",
}

// Brew packages never auto-upgraded.
var criticalPackages = []string{
	"llvm", "gcc", "rust", "go",
	"python@3.11", "python@3.12", "python@3.13",
	"node", "openjdk", "openjdk@17", "openjdk@21",
	"openssl@3", "openssl@1.1", "ca-certificates", "libssh2",
	"zsh", "bash", "fish",
	"curl", "git",
	"postgresql@14", "postgresql@15", "postgresql@16", "postgresql@17",
	"mysql", "mysql@8.0", "sqlite",
	"cmake", "ninja", "pkg-config",
}

// Known clashes / EOL rules for brew-analyze.
type clashRule struct {
	pkg         string
	alternative string
	reason      string
}

var knownClashes = []clashRule{
	{"openssl@1.1", "openssl@3", "openssl@1.1 EOL Sep 2023; migrate dependents to @3"},
	{"python@3.9", "python@3.13", "Python 3.9 EOL Oct 2025"},
	{"python@3.10", "python@3.13", "Python 3.10 EOL Oct 2026"},
	{"node@16", "node", "Node 16 EOL Sep 2023"},
	{"node@18", "node", "Node 18 LTS ended Apr 2025"},
	{"node@20", "node", "Node 20 LTS ends Apr 2026"},
	{"mysql@5.7", "mysql", "MySQL 5.7 EOL Oct 2023"},
	{"mysql@8.0", "mysql", "MySQL 8.0 EOL Apr 2026"},
	{"postgresql@12", "postgresql@17", "PostgreSQL 12 EOL Nov 2024"},
	{"postgresql@13", "postgresql@17", "PostgreSQL 13 EOL Nov 2025"},
	{"postgresql@14", "postgresql@17", "PostgreSQL 14 EOL Nov 2026"},
	{"ruby@2.7", "ruby", "Ruby 2.7 EOL Mar 2023"},
	{"ruby@3.0", "ruby", "Ruby 3.0 EOL Mar 2024"},
	{"php@7.4", "php", "PHP 7.4 EOL Nov 2022"},
	{"php@8.0", "php", "PHP 8.0 EOL Nov 2023"},
	{"php@8.1", "php", "PHP 8.1 EOL Dec 2025"},
	{"curl", "(system)", "Homebrew curl shadows macOS curl in PATH"},
	{"git", "(system)", "Xcode CLT git may conflict; verify which git order"},
	{"zsh", "(system)", "Homebrew zsh can cause startup file confusion"},
	{"llvm@14", "llvm", "LLVM 14 is outdated; check if any formula requires it"},
	{"llvm@15", "llvm", "LLVM 15 is outdated; check if any formula requires it"},
	{"llvm@16", "llvm", "LLVM 16 is outdated; check if any formula requires it"},
	{"gcc@11", "gcc", "GCC 11 slot; check if any formula requires it"},
	{"gcc@12", "gcc", "GCC 12 slot; check if any formula requires it"},
}

const dockerPruneTimeout = 5 * time.Minute
const tmutilFreeTarget = "5000000000"
const tmutilPriority = "4"
