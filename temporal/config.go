package temporal

// TemporalConfig is the configuration for connecting to a Temporal server.
type TemporalConfig struct {
	Host      string `mapstructure:"host"`
	Port      uint16 `mapstructure:"port"`
	Namespace string `mapstructure:"namespace"`
	TaskQueue string `mapstructure:"task_queue"`
}
