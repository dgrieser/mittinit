package config

import "github.com/hashicorp/hcl/v2"

// Config holds the full configuration aggregated from all .hcl files
type Config struct {
	Jobs   []*Job
	Boots  []*Boot
	Probes []*Probe
}

// ConfigFile represents the top-level structure of a single HCL file
type ConfigFile struct {
	Jobs   []*Job   `hcl:"job,block"`
	Boots  []*Boot  `hcl:"boot,block"`
	Probes []*Probe `hcl:"probe,block"`
	Remain hcl.Body `hcl:",remain"`
}

// Job defines a job to be run by mittinit
type Job struct {
	Name                string   `hcl:"name,label"`
	Command             string   `hcl:"command"`
	Args                []string `hcl:"args,optional"`
	MaxAttempts         int      `hcl:"max_attempts,optional"`
	CanFail             bool     `hcl:"can_fail,optional"`
	WorkingDirectory    string   `hcl:"working_directory,optional"`
	Env                 []string `hcl:"env,optional"`
	Stdout              string   `hcl:"stdout,optional"`
	Stderr              string   `hcl:"stderr,optional"`
	EnableTimestamps    bool     `hcl:"enable_timestamps,optional"`
	TimestampFormat     string   `hcl:"timestamp_format,optional"` // Default: "RFC3339"
	CustomTimestampFormat string   `hcl:"custom_timestamp_format,optional"`
	Watch               []Watch  `hcl:"watch,block"`
	Lazy                *Lazy    `hcl:"lazy,block"`
	Listen              []Listen `hcl:"listen,block"`
}

// Watch defines a file or directory to watch for changes
type Watch struct {
	Name        string      `hcl:"name,label"` // Name for the watch block, e.g., "config_file"
	Path        string      `hcl:"path"`       // Path to watch
	Signal      int         `hcl:"signal"`
	Restart     bool        `hcl:"restart,optional"`
	PreCommand  *SubCommand `hcl:"pre_command,block"`
	PostCommand *SubCommand `hcl:"post_command,block"`
}

// SubCommand defines a command to be run as part of a watch or other action
type SubCommand struct {
	Command string   `hcl:"command"`
	Args    []string `hcl:"args,optional"`
}

// Lazy defines parameters for lazy-starting a job
type Lazy struct {
	SpinUpTimeout   string `hcl:"spin_up_timeout"`   // e.g., "10s"
	CoolDownTimeout string `hcl:"cool_down_timeout"` // e.g., "5m"
}

// Listen defines a network address to listen on and forward
type Listen struct {
	Name    string `hcl:"name,label"`    // e.g., "http" from listen "http" { ... }
	Address string `hcl:"address"`       // e.g., "tcp://:8080"
	Forward string `hcl:"forward"`       // e.g., "tcp://127.0.0.1:80"
}

// Boot defines a command to be run at startup
type Boot struct {
	Name    string   `hcl:"name,label"`
	Command string   `hcl:"command"`
	Args    []string `hcl:"args,optional"`
	Timeout string   `hcl:"timeout,optional"` // e.g., "30s"
	Env     []string `hcl:"env,optional"`
}

// Probe defines a health check
type Probe struct {
	Name       string           `hcl:"name,label"`
	Wait       bool             `hcl:"wait,optional"`
	Redis      *RedisProbe      `hcl:"redis,block"`
	Smtp       *SmtpProbe       `hcl:"smtp,block"`
	Mysql      *MysqlProbe      `hcl:"mysql,block"`
	Amqp       *AmqpProbe       `hcl:"amqp,block"`
	Mongodb    *MongodbProbe    `hcl:"mongodb,block"`
	Http       *HttpProbe       `hcl:"http,block"`
	Filesystem *FilesystemProbe `hcl:"filesystem,block"`
}

// RedisProbe defines parameters for a Redis health check
type RedisProbe struct {
	Host     *HostPort `hcl:"host,block"`
	Password string    `hcl:"password,optional"`
}

// SmtpProbe defines parameters for an SMTP health check
type SmtpProbe struct {
	Host *HostPort `hcl:"host,block"`
}

// MysqlProbe defines parameters for a MySQL health check
type MysqlProbe struct {
	Host        *HostPort     `hcl:"host,block"`
	Credentials *UserPassword `hcl:"credentials,block"`
}

// AmqpProbe defines parameters for an AMQP health check
type AmqpProbe struct {
	Host        *HostPort     `hcl:"host,block"`
	Credentials *UserPassword `hcl:"credentials,block"`
	VirtualHost string        `hcl:"virtual_host,optional"`
}

// MongodbProbe defines parameters for a MongoDB health check
type MongodbProbe struct {
	URL string `hcl:"url"`
}

// HttpProbe defines parameters for an HTTP health check
type HttpProbe struct {
	Scheme  string    `hcl:"scheme,optional"` // Default: "http"
	Host    *HostPort `hcl:"host,block"`
	Path    string    `hcl:"path,optional"`    // Default: "/"
	Timeout string    `hcl:"timeout,optional"` // e.g., "5s"
}

// FilesystemProbe defines parameters for a filesystem health check
type FilesystemProbe struct {
	Path string `hcl:"path"`
}

// HostPort defines a hostname and port pair
type HostPort struct {
	Hostname string `hcl:"hostname"`
	Port     int    `hcl:"port"`
}

// UserPassword defines a username and password pair
type UserPassword struct {
	User     string `hcl:"user"`
	Password string `hcl:"password"`
}
