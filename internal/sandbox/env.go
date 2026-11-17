package sandbox

// The bot's home lives in its persistent workspace: settings, application
// data, `pip install --user` packages, `npm -g` tools, AppImages and the
// Debian packages `userapt` unpacks all survive sleep and restarts. Caches
// stay on the container's tmpfs so they never grow the workspace. The image
// sets the same values (deploy/images/computer/Dockerfile, env.sh); the
// worker passes them explicitly so a computer gets them whatever its image.
const (
	// HomeDir is $HOME inside every computer. It is hidden (a dot
	// directory) so the workspace views and the Library indexer skip it.
	HomeDir = "/workspace/.home"
	// UserAptRoot is where userapt unpacks Debian packages.
	UserAptRoot = HomeDir + "/.local/apt"
	// JobsDir holds background job records and logs (see jobs.go).
	JobsDir = "/workspace/.superbot/jobs"
)

// computerPath puts the bot's own programs in front of the system's.
const computerPath = HomeDir + "/.local/bin:" +
	UserAptRoot + "/usr/bin:" + UserAptRoot + "/bin:" +
	"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// computerLibraryPath only holds libraries userapt unpacked, which it never
// does for a library the image already has.
const computerLibraryPath = UserAptRoot + "/usr/lib/x86_64-linux-gnu:" +
	UserAptRoot + "/lib/x86_64-linux-gnu:" + UserAptRoot + "/usr/lib"

// computerEnv is the environment of the computer's processes, both the
// container's own and every exec the tools start.
func computerEnv() []string {
	return []string{
		"HOME=" + HomeDir,
		"TMPDIR=/tmp",
		"DISPLAY=:99",
		"TZ=CST-8",
		"LANG=C.UTF-8",
		"PATH=" + computerPath,
		"LD_LIBRARY_PATH=" + computerLibraryPath,
		"XDG_DATA_DIRS=" + UserAptRoot + "/usr/share:/usr/local/share:/usr/share",
		"XDG_CACHE_HOME=/tmp/.cache",
		"XDG_RUNTIME_DIR=/tmp/.runtime",
		"PYTHONDONTWRITEBYTECODE=1",
		"PIP_NO_CACHE_DIR=1",
		// Debian marks the system Python as externally managed; with this
		// `pip install` falls back to a --user install in the persistent
		// home instead of refusing. Virtual environments are unaffected.
		"PIP_BREAK_SYSTEM_PACKAGES=1",
		"NPM_CONFIG_PREFIX=" + HomeDir + "/.local",
		"NPM_CONFIG_CACHE=/tmp/.npm",
		// AppImages unpack themselves instead of needing FUSE.
		"APPIMAGE_EXTRACT_AND_RUN=1",
		// GTK programs otherwise look for an accessibility bus that does
		// not exist and print warnings into every log.
		"NO_AT_BRIDGE=1",
	}
}
