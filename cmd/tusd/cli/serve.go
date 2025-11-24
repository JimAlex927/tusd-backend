package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	tushandler "github.com/JimAlex927/tusd-backend/pkg/handler"
	"github.com/JimAlex927/tusd-backend/pkg/hooks"
	"github.com/JimAlex927/tusd-backend/pkg/hooks/plugin"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	TLS13       = "tls13"
	TLS12       = "tls12"
	TLS12STRONG = "tls12-strong"
)

// Setups the different components, starts a Listener and give it to
// http.Serve().
//启动不同的 组成单元，开启一个监听器，并且 调用 http.Serve方法

// 默认情况下，它会绑定一个指定的 host/port ,除非 一个 unix socket 被指定，这种情况下 一个不同的 socket创建和绑定机制 is put in place.
// By default it will bind to the specified host/port, unless a UNIX socket is
// specified, in which case a different socket creation and binding mechanism
// is put in place.
func Serve() {

	//这里是 定义配置
	config := tushandler.Config{
		DeletePartAfterConcatenation:     Flags.DeletePartAfterConcatenation,
		ComposerCoreType:                 ComposerType,
		EnableUploadCopyPath:             Flags.EnableUploadCopyPath,
		MaxSize:                          Flags.MaxSize,
		BasePath:                         Flags.Basepath,
		Cors:                             getCorsConfig(),
		RespectForwardedHeaders:          Flags.BehindProxy,
		EnableExperimentalProtocol:       Flags.ExperimentalProtocol,
		DisableDownload:                  Flags.DisableDownload,
		DisableTermination:               Flags.DisableTermination,
		DisableConcatenation:             Flags.DisableConcatenation,
		StoreComposer:                    Composer,
		UploadProgressInterval:           Flags.ProgressHooksInterval,
		AcquireLockTimeout:               Flags.AcquireLockTimeout,
		GracefulRequestCompletionTimeout: Flags.GracefulRequestCompletionTimeout,
		NetworkTimeout:                   Flags.NetworkTimeout,
	}
	//第一个 handler
	var handler *tushandler.Handler
	var err error
	//第二个 是  hookhandler
	hookHandler := getHookHandler(&config)
	if hookHandler != nil {
		//这里 如果 hookhandler 不是空的 就会 把hookhandler转换成 handler
		handler, err = hooks.NewHandlerWithHooks(&config, hookHandler, Flags.EnabledHooks)

		var enabledHooksString []string
		for _, h := range Flags.EnabledHooks {
			enabledHooksString = append(enabledHooksString, string(h))
		}

		printStartupLog("Enabled hook events: %s", strings.Join(enabledHooksString, ", "))

	} else {
		//如果hook handler是空的 就会根据 上面的配置文件创建一个handler
		handler, err = tushandler.NewHandler(config)
	}
	if err != nil {
		stderr.Fatalf("Unable to create handler: %s", err)
	}
	//-----------------以上的过程是 创建 一个  handler 的过程------------
	//打印日志
	printStartupLog("Supported tus extensions: %s\n", handler.SupportedExtensions())

	basepath := Flags.Basepath
	//---------------------这一部分是决定最终的 监听端口 和地址-----------------------
	address := ""
	if Flags.HttpSock != "" {
		address = Flags.HttpSock
		printStartupLog("Using %s as socket to listen.\n", address)
	} else {
		//如果没指定address 也就是 命令行参数没有指定 address 就会使用默认的 port 0.0.0.0:8080
		address = Flags.HttpHost + ":" + Flags.HttpPort
		printStartupLog("Using %s as address to listen.\n", address)
	}
	//------------------------------------------------------------------
	printStartupLog("Using %s as the base path.\n", basepath)

	//todo http.NewServeMux() 创建了一个“路由表”，后面的所有 mux.Handle(...) 就是往这张表里注册：哪条路径该由哪个 handler 来处理。
	mux := http.NewServeMux()

	if basepath == "/" {
		//如果basepath被设置成了 根路径 `/` 只会安装 tusd 的handler 不会 展示 欢迎
		// If the basepath is set to the root path, only install the tusd handler
		// and do not show a greeting.
		// note： 这里也就是说  把 "/" 直接全部交由 handler 处理了
		mux.Handle("/", http.StripPrefix("/", handler))
	} else {
		//note： 如果 自定义basepath，我们在根路径下 展示一个 greeting ，这句话的意思就是 在 根页面，也就是网页上 是否展示欢迎 哈哈
		// If a custom basepath is defined, we show a greeting at the root path...
		// note 这里 可以通过命令行参数 在定制页面 展示 欢迎。 Flags.ShowGreeting
		if Flags.ShowGreeting {
			mux.HandleFunc("/", DisplayGreeting)
		}

		// ... and register a route with and without the trailing slash, so we can
		// handle uploads for /files/ and /files, for example.
		//note  这里的 basepath =  "/files/"  .
		//移除字符串末尾的指定后缀 "/files/"  -->> "/files"
		basepathWithoutSlash := strings.TrimSuffix(basepath, "/")
		basepathWithSlash := basepathWithoutSlash + "/"

		//这两行就是说 把 "/files/" 和 "/files" 都注册 给  上面定义好的 handler
		mux.Handle(basepathWithSlash, http.StripPrefix(basepathWithSlash, handler))
		mux.Handle(basepathWithoutSlash, http.StripPrefix(basepathWithoutSlash, handler))
	}

	if Flags.ExposeMetrics {
		//note 这里是 给 tusd 加上 prometheus 的监控能力
		SetupMetrics(mux, handler)
		hooks.SetupHookMetrics()
	}

	if Flags.ExposePprof {
		//这函数 SetupPprof 是给 tusd 加上 Go 官方最强性能调试神器 —— pprof 的关键代码。
		SetupPprof(mux)
	}
	//-------------创建了真正的网络监听器Listener（TCP 或者 Unix Socket），
	var listener net.Listener
	if Flags.HttpSock != "" {
		listener, err = NewUnixListener(address)
	} else {
		listener, err = NewListener(address)
	}

	if err != nil {
		stderr.Fatalf("Unable to create listener: %s", err)
	}
	//----------------------------------------------------------
	//-------------------------协议是 http 还是 https------------
	protocol := "http"
	if Flags.TLSCertFile != "" && Flags.TLSKeyFile != "" {
		protocol = "https"
	}

	if Flags.HttpSock == "" {
		printStartupLog("You can now upload files to: %s://%s%s", protocol, listener.Addr(), basepath)
	}
	// 2. 把前面建好的 mux（包含所有路由）塞进 http.Server ，Listener 和 handler绑定

	//是 tusd 实现“优雅关闭（Graceful Shutdown）”的核心灵魂，一句话总结：
	//它创建了一个可以“带原因取消”的 context，等收到 kill 信号时，
	//主动取消这个 context，让所有正在上传的分片请求立刻知道「服务器要关了」，从而快速结束、释放锁、写完最后一点数据，而不是被粗暴砍断。
	//# 你按 Ctrl+C 或 systemctl stop tusd：
	//		→ 操作系统发 SIGINT/SIGTERM
	//		→ setupSignalHandler() 捕获信号
	//		→ cancelServerCtx(tushandler.ErrServerShutdown)   ← 关键触发！
	//		→ serverCtx.Done() 通道立刻关闭
	//		→ 所有正在进行的 PATCH 请求（正在 io.Copy 写磁盘）立刻收到 ctx.Done()
	//		→ tusd 内部会：
	//		• 停止读取请求体
	//		• 释放这个上传 ID 的锁
	//		• 返回错误给客户端（通常是 500 或连接断开）
	//		• 让 http.Server.Shutdown() 能快速完成
	//		→ 整个进程最长等 Flags.ShutdownTimeout（默认 10 秒）就彻底退出
	serverCtx, cancelServerCtx := context.WithCancelCause(context.Background())
	//创建一个server
	server := &http.Server{
		Handler: mux,
		// ReadHeaderTimeout is the timeout for reading the entire request
		// header. This does not include reading a potential request body.
		ReadHeaderTimeout: Flags.NetworkTimeout,
		// ReadTimeout and WriteTimeout are absolute values that govern when
		// reads/writes time out. Since the incoming requests have a flexible duration,
		// we do not rely on these absolute values, but extend the timeouts
		// dynamically as needed in UnroutedHandler via http.ResponseControler.SetRead/WriteDeadline.
		ReadTimeout:    0,
		WriteTimeout:   0,
		IdleTimeout:    Flags.NetworkTimeout,
		MaxHeaderBytes: http.DefaultMaxHeaderBytes,
		ConnState: func(_ net.Conn, cs http.ConnState) {
			switch cs {
			case http.StateNew:
				MetricsOpenConnections.Inc()
			case http.StateClosed, http.StateHijacked:
				MetricsOpenConnections.Dec()
			}
		},
		BaseContext: func(_ net.Listener) context.Context {
			return serverCtx
		},
	}
	//还是关闭的信号操作  shutdownComplete 是一个管道
	shutdownComplete := setupSignalHandler(server, cancelServerCtx)

	if protocol == "http" {
		// Non-TLS mode
		//这里走的是http请求的时候
		//如果用的是http2  EnableH2C 就会使用 http2 的服务器
		if Flags.EnableH2C {
			// Wrap in h2c for optional HTTP/2 support in clear text mode (without TLS)
			// See https://pkg.go.dev/golang.org/x/net/http2/h2c#NewHandler
			h2s := &http2.Server{}
			newHandler := h2c.NewHandler(mux, h2s)
			server.Handler = newHandler
		}
		// 把 server 和 listener 绑定在一起，也就是这个server去绑定对应的网络端口
		err = server.Serve(listener)
	} else {
		// TLS mode
		//走的https了  ：  http + TLS
		err = serveTLS(server, listener)
	}

	// Note: http.Server.Serve and http.Server.ServeTLS (in serveTLS) always return a non-nil error code. So
	// we can assume from here that `err != nil`
	//上面说 不管是http 还是http是 返回的 error 一定是非空的 所以下面不需要进行 =nil的判断
	if err == http.ErrServerClosed {
		// ErrServerClosed means that http.Server.Shutdown was called due to an interruption signal.
		// We wait until the interruption procedure is complete or times out and then exit main.
		//shutdownComplete 是一个管道 这里发现报错了 会先等待 错误处理完成才进行下一步
		<-shutdownComplete
	} else {
		// Any other error is relayed to the user.
		stderr.Fatalf("Unable to serve: %s", err)
	}
}

func serveTLS(server *http.Server, listener net.Listener) error {
	switch Flags.TLSMode {
	case TLS13:
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS13}

	case TLS12:
		// Ciphersuite selection comes from
		// https://ssl-config.mozilla.org/#server=go&version=1.14.4&config=intermediate&guideline=5.6
		// 128-bit AES modes remain as TLSv1.3 is enabled in this mode, and TLSv1.3 compatibility requires an AES-128 ciphersuite.
		server.TLSConfig = &tls.Config{
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			},
		}

	case TLS12STRONG:
		// Ciphersuite selection as above, but intersected with
		// https://github.com/denji/golang-tls#perfect-ssl-labs-score-with-go
		// TLSv1.3 is disabled as it requires an AES-128 ciphersuite.
		server.TLSConfig = &tls.Config{
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			},
		}

	default:
		stderr.Fatalf("Invalid TLS mode chosen. Recommended valid modes are tls13, tls12 (default), and tls12-strong")
	}

	// Disable HTTP/2; the default non-TLS mode doesn't support it
	server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0)

	return server.ServeTLS(listener, Flags.TLSCertFile, Flags.TLSKeyFile)
}

func setupSignalHandler(server *http.Server, cancelServerCtx context.CancelCauseFunc) <-chan struct{} {
	shutdownComplete := make(chan struct{})

	// We read up to two signals, so use a capacity of 2 here to not miss any signal
	c := make(chan os.Signal, 2)

	// os.Interrupt is mapped to SIGINT on Unix and to the termination instructions on Windows.
	// On Unix we also listen to SIGTERM.
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// When closing the server, cancel its context so all open requests shut down as well.
	// See context.go for the logic.
	server.RegisterOnShutdown(func() {
		cancelServerCtx(tushandler.ErrServerShutdown)
	})

	go func() {
		// First interrupt signal
		<-c
		stdout.Println("Received interrupt signal. Shutting down tusd...")

		// Wait for second interrupt signal, while also shutting down the existing server
		go func() {
			<-c
			stdout.Println("Received second interrupt signal. Exiting immediately!")
			os.Exit(1)
		}()

		// Shutdown the server, but with a user-specified timeout
		ctx, cancel := context.WithTimeout(context.Background(), Flags.ShutdownTimeout)
		defer cancel()

		err := server.Shutdown(ctx)

		if err == nil {
			stdout.Println("Shutdown completed. Goodbye!")
		} else if errors.Is(err, context.DeadlineExceeded) {
			stderr.Println("Shutdown timeout exceeded. Exiting immediately!")
		} else {
			stderr.Printf("Failed to shutdown gracefully: %s\n", err)
		}

		// Make sure that the plugins exit properly.
		plugin.CleanupPlugins()

		close(shutdownComplete)
	}()

	return shutdownComplete
}

func getCorsConfig() *tushandler.CorsConfig {
	config := tushandler.DefaultCorsConfig
	config.Disable = Flags.DisableCors
	config.AllowCredentials = Flags.CorsAllowCredentials
	config.MaxAge = Flags.CorsMaxAge

	var err error
	config.AllowOrigin, err = regexp.Compile(Flags.CorsAllowOrigin)
	if err != nil {
		stderr.Fatalf("Invalid regular expression for -cors-allow-origin flag: %s", err)
	}

	if Flags.CorsAllowHeaders != "" {
		config.AllowHeaders += ", " + Flags.CorsAllowHeaders
	}

	if Flags.CorsAllowMethods != "" {
		config.AllowMethods += ", " + Flags.CorsAllowMethods
	}

	if Flags.CorsExposeHeaders != "" {
		config.ExposeHeaders += ", " + Flags.CorsExposeHeaders
	}

	return &config
}
