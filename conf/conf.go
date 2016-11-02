package conf

// DaemonConfig - Organise attributes that can be modified by command line parameter of by API call
type DaemonConfig struct {
  BaseDir       string
  SocketPath    string
  JobConfigPath string
  LoggingPath   string
  LogLevel      int
  Port          int
  APIAddress    string
  APIPort       int
  APITimeout    int
}

var Attr = DaemonConfig{}

func init() {

  // Set default configurations
  Attr.BaseDir = "/opt/omicrond"
  Attr.SocketPath = Attr.BaseDir + "omicrond.sock"
  Attr.JobConfigPath = "sample/samplejobConf.toml"
  Attr.LoggingPath = Attr.BaseDir + "/logs"
  Attr.LogLevel = 0
  Attr.Port = 51515
  Attr.APIAddress = "127.0.0.1"
  Attr.APIPort = 12221
  Attr.APITimeout = 5

}