package config

import "fmt"

type ArchiveCLIInput struct {
	All          bool
	VersionsFile string
	Versions     []string
	Includes     ArchiveIncludes
	IncludeAll   bool
}

func (c *Config) ApplyArchiveCLI(in ArchiveCLIInput) error {
	if !in.All && in.VersionsFile == "" && len(in.Versions) == 0 && !in.IncludeAll &&
		!in.Includes.Snapshots && !in.Includes.PreReleases && !in.Includes.RC && !in.Includes.Alpha && !in.Includes.Beta {
		return nil
	}
	if in.All && (in.VersionsFile != "" || len(in.Versions) > 0) {
		return fmt.Errorf("--all cannot be combined with version arguments")
	}
	if in.VersionsFile != "" && len(in.Versions) > 0 {
		return fmt.Errorf("use either --versions-file or version arguments, not both")
	}
	if in.IncludeAll {
		c.SetArchiveIncludes(ArchiveIncludes{
			Snapshots: true, PreReleases: true, RC: true, Alpha: true, Beta: true,
		})
	} else if in.Includes.Snapshots || in.Includes.PreReleases || in.Includes.RC || in.Includes.Alpha || in.Includes.Beta {
		c.SetArchiveIncludes(in.Includes)
	}
	if in.All || in.IncludeAll || in.Includes.Snapshots || in.Includes.PreReleases || in.Includes.RC || in.Includes.Alpha || in.Includes.Beta {
		c.ArchiveAll = true
		c.VersionsFile = ""
		c.Versions = nil
		return nil
	}
	if in.VersionsFile != "" {
		c.VersionsFile = in.VersionsFile
		c.Versions = nil
		return nil
	}
	c.Versions = in.Versions
	c.VersionsFile = ""
	return nil
}

func ReadVersionList(path string) ([]string, error) {
	return readLines(path)
}
