package config

import (
	"os"
	"strconv"
)

type Runtime struct {
	DatabaseURL              string
	DatabaseDriver           string
	ProjectPath              string
	SuppressBootstrapWarning bool
	IndexLinesPerChunk       int
}

func Resolve(input Runtime) Runtime {
	out := input

	if out.DatabaseURL == "" {
		out.DatabaseURL = os.Getenv("AGENTDB_DB_URL")
	}
	if out.DatabaseURL == "" {
		out.DatabaseURL = LoadDefaultDatabaseURL()
	}
	if out.DatabaseURL == "" {
		out.DatabaseURL = DefaultDatabasePath()
	}
	out.DatabaseURL = expandTilde(out.DatabaseURL)

	if out.DatabaseDriver == "" {
		out.DatabaseDriver = os.Getenv("AGENTDB_DB_DRIVER")
	}
	if out.DatabaseDriver == "" {
		out.DatabaseDriver = LoadDefaultDatabaseDriver()
	}
	if out.DatabaseDriver == "" {
		out.DatabaseDriver = "auto"
	}

	if out.ProjectPath == "" {
		out.ProjectPath = os.Getenv("AGENTDB_PROJECT_PATH")
	}
	if out.ProjectPath == "" {
		out.ProjectPath = LoadDefaultProjectPath()
	}
	out.ProjectPath = expandTilde(out.ProjectPath)

	if out.IndexLinesPerChunk == 0 {
		rawLinesPerChunk := os.Getenv("AGENTDB_LINES_PER_CHUNK")
		if rawLinesPerChunk != "" {
			parsed, err := strconv.Atoi(rawLinesPerChunk)
			if err == nil {
				out.IndexLinesPerChunk = parsed
			}
		}
	}
	if out.IndexLinesPerChunk == 0 {
		out.IndexLinesPerChunk = LoadDefaultLinesPerChunk()
	}
	if out.IndexLinesPerChunk <= 0 {
		out.IndexLinesPerChunk = 50
	}

	return out
}
