package config

type ArchiveIncludes struct {
	Snapshots   bool
	PreReleases bool
	RC          bool
	Alpha       bool
	Beta        bool
}

func (c *Config) AllowsVersionChannel(channel string) bool {
	switch channel {
	case "release":
		return true
	case "snapshot":
		return c.IncludeSnapshots
	case "prerelease":
		return c.IncludePreReleases
	case "rc":
		return c.IncludeRC
	case "alpha":
		return c.IncludeAlpha
	case "beta":
		return c.IncludeBeta
	default:
		return false
	}
}

func (c *Config) SetArchiveIncludes(in ArchiveIncludes) {
	c.IncludeSnapshots = in.Snapshots
	c.IncludePreReleases = in.PreReleases
	c.IncludeRC = in.RC
	c.IncludeAlpha = in.Alpha
	c.IncludeBeta = in.Beta
}

func (c *Config) IncludesAllOptionalTypes() bool {
	return c.IncludeSnapshots && c.IncludePreReleases && c.IncludeRC && c.IncludeAlpha && c.IncludeBeta
}
