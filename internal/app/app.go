package app

// App represents the top-level AegisKeys application.
type App struct {
	// ConfigPath holds the path to the config directory.
	ConfigPath string
	// Initialized indicates whether the user has run setup.
	Initialized bool
}
