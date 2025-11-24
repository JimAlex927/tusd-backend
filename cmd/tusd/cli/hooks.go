package cli

import (
	"strings"

	"github.com/JimAlex927/tusd-backend/pkg/handler"
	"github.com/JimAlex927/tusd-backend/pkg/hooks"
	"github.com/JimAlex927/tusd-backend/pkg/hooks/file"
	"github.com/JimAlex927/tusd-backend/pkg/hooks/grpc"
	"github.com/JimAlex927/tusd-backend/pkg/hooks/http"
	"github.com/JimAlex927/tusd-backend/pkg/hooks/plugin"
)

func getHookHandler(config *handler.Config) hooks.HookHandler {
	if Flags.FileHooksDir != "" {
		printStartupLog("Using '%s' for hooks", Flags.FileHooksDir)

		return &file.FileHook{
			Directory: Flags.FileHooksDir,
		}
	} else if Flags.HttpHooksEndpoint != "" {
		printStartupLog("Using '%s' as the endpoint for hooks", Flags.HttpHooksEndpoint)

		return &http.HttpHook{
			Endpoint:       Flags.HttpHooksEndpoint,
			MaxRetries:     Flags.HttpHooksRetry,
			Backoff:        Flags.HttpHooksBackoff,
			ForwardHeaders: strings.Split(Flags.HttpHooksForwardHeaders, ","),
			Timeout:        Flags.HttpHooksTimeout,
			SizeLimit:      Flags.HttpHooksSizeLimit,
		}
	} else if Flags.GrpcHooksEndpoint != "" {
		printStartupLog("Using '%s' as the endpoint for gRPC hooks", Flags.GrpcHooksEndpoint)

		return &grpc.GrpcHook{
			Endpoint:                        Flags.GrpcHooksEndpoint,
			MaxRetries:                      Flags.GrpcHooksRetry,
			Backoff:                         Flags.GrpcHooksBackoff,
			Secure:                          Flags.GrpcHooksSecure,
			ServerTLSCertificateFilePath:    Flags.GrpcHooksServerTLSCertFile,
			ClientTLSCertificateFilePath:    Flags.GrpcHooksClientTLSCertFile,
			ClientTLSCertificateKeyFilePath: Flags.GrpcHooksClientTLSKeyFile,
			ForwardHeaders:                  strings.Split(Flags.GrpcHooksForwardHeaders, ","),
		}
	} else if Flags.PluginHookPath != "" {
		printStartupLog("Using '%s' to load plugin for hooks", Flags.PluginHookPath)

		return &plugin.PluginHook{
			Path:          Flags.PluginHookPath,
			JSONLogFormat: Flags.LogFormat == "json",
		}
	} else {
		return nil
	}
}
