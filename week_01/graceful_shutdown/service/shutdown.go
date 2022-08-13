package service

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

// Option 典型的 Option 设计模式
type Option func(app *App)

// ShutdownCallback 采用 context.Context 来控制超时，而不是用 time.After 是因为
// - 超时本质上是使用这个回调的人控制的
// - 我们还希望用户知道，他的回调必须要在一定时间内处理完毕，而且他必须显式处理超时错误
type ShutdownCallback func(ctx context.Context)

// WithShutdownCallbacks 你需要实现这个方法
func WithShutdownCallbacks(cbs ...ShutdownCallback) Option {
	return func(app *App) {
		app.cbs = cbs
	}
}

// App 这里我已经预先定义好了各种可配置字段
type App struct {
	servers []*Server

	// 优雅退出整个超时时间，默认30秒
	shutdownTimeout time.Duration

	// 优雅退出时候等待处理已有请求时间，默认10秒钟
	waitTime time.Duration
	// 自定义回调超时时间，默认三秒钟
	cbTimeout time.Duration

	cbs []ShutdownCallback
}

// NewApp 创建 App 实例，注意设置默认值，同时使用这些选项
func NewApp(servers []*Server, opts ...Option) *App {
	var app = &App{
		shutdownTimeout: 30 * time.Second,
		waitTime:        10 * time.Second,
		cbTimeout:       3 * time.Second,
	}
	log.Printf("默认关闭时间为%s", app.shutdownTimeout)
	log.Printf("默认等待时间为%s", app.waitTime)
	log.Printf("默认回调超时时间为%s", app.cbTimeout)
	for _, srv := range servers {
		app.servers = append(app.servers, srv)
	}
	for _, opt := range opts {
		opt(app)
	}
	return app
}

// StartAndServe 你主要要实现这个方法
func (app *App) StartAndServe() {
	for _, s := range app.servers {
		srv := s
		go func() {
			if err := srv.Start(); err != nil {
				if err == http.ErrServerClosed {
					log.Printf("服务器%s已关闭", srv.name)
				} else {
					log.Printf("服务器%s异常退出", srv.name)
				}

			}
		}()
	}
	log.Println("服务器启动")
	// 从这里开始优雅退出监听系统信号，强制退出以及超时强制退出。
	// 优雅退出的具体步骤在 shutdown 里面实现
	// 所以你需要在这里恰当的位置，调用 shutdown
	var done = make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, os.Kill)
	select {
	case <-done:
		log.Println("===========啊啊啊啊啊,我收到退出信号了，给我 30s，最多 30s===========")
		ctx, cancel := context.WithTimeout(context.Background(), app.shutdownTimeout)
		defer cancel()
		go app.shutdown(ctx)
		log.Println("我已经安排人去处理你的请求了，你等等奥，等不及可以再来一次 ctrl + c")
		//signal.Notify(done, os.Interrupt, os.Kill)
		select {
		case <-done:
			log.Println("===========我收到了第二个退出信号，你好狠心，就不能等等人家===========")
			os.Exit(127)
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				log.Println("===========你看，我利用 ctx 最多 30 s shutdown===========")
				os.Exit(1)
			}
		}
	}
}

// shutdown 你要设计这里面的执行步骤。
func (app *App) shutdown(ctx context.Context) {
	log.Println("在接受到 ctrl+c 后，我们进行优雅退出")
	//var done = make(chan interface{}, len(app.servers))
	for _, srv := range app.servers {
		// 你需要在这里让所有的 server 拒绝新请求
		log.Printf("%s server 开始拒绝新请求", srv.name)
		srv.rejectReq()
		log.Printf("%s server 等待已有请求服务处理完毕，10s 吧", srv.name)
		//log.Println("等待正在执行请求完结")
		time.AfterFunc(app.waitTime, srv.Close)
	}
	// TODO 是否能从 AfterFunc 中获取到 srv close 的信号？要不还得手动 sleep 一下
	time.Sleep(app.waitTime)    // 这里处理感觉很怪，应该想办法从 AfterFunc 中获取
	time.Sleep(1 * time.Second) // 再稍微等一下，要不 srv 的 close 信息和后面的信息重叠了
	log.Println("开始执行自定义回调")
	wg := &sync.WaitGroup{}
	ctx2, cancel2 := context.WithTimeout(ctx, app.cbTimeout)
	defer cancel2()
	for _, cb := range app.cbs {
		// 并发执行回调，要注意协调所有的回调都执行完才会步入下一个阶段
		wg.Add(1)
		go func(cb ShutdownCallback) {
			defer wg.Done()
			cb(ctx2)
		}(cb)
	}
	// 在这里等待一段时间
	wg.Wait()
	log.Println("所有回调执行完毕")
	// 并发关闭服务器，同时要注意协调所有的 server 都关闭之后才能步入下一个阶段
	// 释放资源
	log.Println("开始释放资源,如 DB 连接等")
	app.close()
}

func (app *App) close() {
	// 在这里释放掉一些可能的资源
	log.Println("应用关闭")
	os.Exit(130)
}

type Server struct {
	srv  *http.Server
	name string
	mux  *serverMux
}

// serverMux 既可以看做是装饰器模式，也可以看做委托模式
type serverMux struct {
	reject bool
	*http.ServeMux
}

func (s *serverMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.reject {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("服务已关闭"))
		return
	}
	s.ServeMux.ServeHTTP(w, r)
}

func NewServer(name string, addr string) *Server {
	mux := &serverMux{ServeMux: http.NewServeMux()}
	return &Server{
		name: name,
		mux:  mux,
		srv: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

func (s *Server) Handle(pattern string, handler http.Handler) {
	s.mux.Handle(pattern, handler)
}

func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) rejectReq() {
	s.mux.reject = true
}

func (s *Server) stop() error {
	log.Printf("服务器%s关闭中", s.name)
	return s.srv.Shutdown(context.Background())
}
func (s *Server) Close() {
	err := s.srv.Close()
	if err != nil {
		log.Printf("服务器%s关闭失败", s.name)
	} else {
		log.Printf("服务器%s关闭成功", s.name)
	}
}
