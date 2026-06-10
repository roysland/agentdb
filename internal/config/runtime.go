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

	EmbeddingProvider       string
	EmbeddingBaseURL        string
	EmbeddingAPIKey         string
	EmbeddingModel          string
	EmbeddingTimeoutSeconds int
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

	if out.EmbeddingProvider == "" {
		out.EmbeddingProvider = os.Getenv("AGENTDB_EMBED_PROVIDER")
	}
	if out.EmbeddingProvider == "" {
		out.EmbeddingProvider = LoadDefaultEmbedProvider()
	}
	if out.EmbeddingProvider == "" {
		out.EmbeddingProvider = "disabled"
	}

	if out.EmbeddingBaseURL == "" {
		out.EmbeddingBaseURL = os.Getenv("AGENTDB_EMBED_BASE_URL")
	}
	if out.EmbeddingBaseURL == "" {
		out.EmbeddingBaseURL = LoadDefaultEmbedBaseURL()
	}
	if out.EmbeddingBaseURL == "" {
		out.EmbeddingBaseURL = "http://localhost:11434/v1"
	}

	if out.EmbeddingAPIKey == "" {
		out.EmbeddingAPIKey = os.Getenv("AGENTDB_EMBED_API_KEY")
	}
	if out.EmbeddingAPIKey == "" {
		out.EmbeddingAPIKey = LoadDefaultEmbedAPIKey()
	}

	if out.EmbeddingModel == "" {
		out.EmbeddingModel = os.Getenv("AGENTDB_EMBED_MODEL")
	}
	if out.EmbeddingModel == "" {
		out.EmbeddingModel = LoadDefaultEmbedModel()
	}
	if out.EmbeddingModel == "" {
		out.EmbeddingModel = "nomic-embed-text"
	}

	if out.EmbeddingTimeoutSeconds == 0 {
		rawTimeout := os.Getenv("AGENTDB_EMBED_TIMEOUT_SECONDS")
		if rawTimeout != "" {
			parsed, err := strconv.Atoi(rawTimeout)
			if err == nil {
				out.EmbeddingTimeoutSeconds = parsed
			}
		}
	}
	if out.EmbeddingTimeoutSeconds == 0 {
		out.EmbeddingTimeoutSeconds = LoadDefaultEmbedTimeoutSeconds()
	}
	if out.EmbeddingTimeoutSeconds <= 0 {
		out.EmbeddingTimeoutSeconds = 30
	}

	return out
}
