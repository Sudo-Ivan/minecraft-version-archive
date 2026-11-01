package buildinfo

var Version = "dev"

func UserAgent() string {
	return "mva/" + Version
}
