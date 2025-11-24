package handler

import (
	"net/http"
	"strings"
)

// Handler is a ready to use handler with routing
type Handler struct {
	*UnroutedHandler
	http.Handler
}

// NewHandler creates a routed tus protocol handler. This is the simplest
// way to use tusd but may not be as configurable as you require. If you are
// integrating this into an existing app you may like to use tusd.NewUnroutedHandler
// instead. Using tusd.NewUnroutedHandler allows the tus handlers to be combined into
// your existing router (aka mux) directly. It also allows the GET and DELETE
// endpoints to be customized. These are not part of the protocol so can be
// changed depending on your needs.
func NewHandler(config Config) (*Handler, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	handler, err := NewUnroutedHandler(config)
	if err != nil {
		return nil, err
	}

	//note：这里根据 unroutedHandler 创建了一个 RoutedHandler ,这里只是引用一下
	//核心还是这个  unroutedHandler ，最开始 的版本，unroutedHandler 自行实现 ServeHttp的接口，但是为了兼容新版本 ，给它包了一层
	routedHandler := &Handler{
		UnroutedHandler: handler,
	}

	//todo 这是一个函数  但是 它可以把一个普通函数 变成一个 http.Handler 的接口类型
	//怎么实现的呢，原理是这样的，定义一个 type ，类型是 一个 函数，也就是说这个 type 是一个type 也是一个需要实现的函数。
	//同时对这个 type实现 接口要求的函数，也就是 通过指针 调用自身 ，这样就实现了  函数 转函数了 ，也就实现了接口！！！ 这是一个很优雅、高级的方法。
	//todo 这个mux 才是核心逻辑 ，准确的来说，结合后面的 middleware -->> mux -->> 分发请求
	mux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.Method
		//strings.Trim函数用于移除字符串开头和结尾的指定字符集合中的任意字符。
		//"/files/" -->> "file"
		path := strings.Trim(r.URL.Path, "/")

		switch path {
		case "":
			// Root endpoint for upload creation
			switch method {
			case "POST":
				// POST 方法 进行 postfile方法
				handler.PostFile(w, r)
			default:
				w.Header().Add("Allow", "POST")
				w.WriteHeader(http.StatusMethodNotAllowed)
				w.Write([]byte(`method not allowed`))
			}
		default:
			// URL points to an upload resource
			switch {
			case method == "HEAD" && r.URL.Path != "":
				// Offset retrieval
				handler.HeadFile(w, r)
			case method == "PATCH" && r.URL.Path != "":
				// Upload apppending
				handler.PatchFile(w, r)
			case method == "GET" && r.URL.Path != "" && !config.DisableDownload:
				// Upload download
				handler.GetFile(w, r)
			case method == "DELETE" && r.URL.Path != "" && config.StoreComposer.UsesTerminater && !config.DisableTermination:
				// Upload termination
				handler.DelFile(w, r)
			default:
				// TODO: Only add GET and DELETE if they are supported
				w.Header().Add("Allow", "GET, HEAD, PATCH, DELETE")
				w.WriteHeader(http.StatusMethodNotAllowed)
				w.Write([]byte(`method not allowed`))
			}
		}
	})
	//todo 这个middleware是对 请求做了一层拦截 处理 ，然后再 交给了 mux 这个 Handler 调用 serveHttp方法！！！
	routedHandler.Handler = handler.Middleware(mux)

	return routedHandler, nil
}
