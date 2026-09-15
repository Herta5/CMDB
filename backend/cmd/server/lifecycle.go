// 本文件协调 HTTP 监听与同步工作器关闭，确保退出前为失败结果落库保留有限时间。
package main

import (
	"cmdb/internal/resource"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// runHTTP 在信号或监听故障后停止受理任务，排队任务由下次启动恢复。
func runHTTP(ctx context.Context, server *http.Server, service *resource.Service) error {
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe() }()
	var serveErr error
	select {
	case serveErr = <-served:
	case <-ctx.Done():
	}
	service.Stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	if shutdownErr != nil {
		_ = server.Close()
	}
	workersDone := make(chan struct{})
	go func() { service.Wait(); close(workersDone) }()
	select {
	case <-workersDone:
	case <-shutdown.Done():
		return errors.New("等待同步工作器退出超时")
	}
	if shutdownErr != nil {
		return errors.New("HTTP 服务关闭超时")
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.New("HTTP 服务监听失败")
	}
	slog.Info("服务已停止", "event", "server_stopped")
	return nil
}

// stopAndWait 在调度器已启动但 HTTP 尚未装配成功时，也给予工作器有限的失败收敛机会。
func stopAndWait(service *resource.Service) error {
	service.Stop()
	done := make(chan struct{})
	go func() { service.Wait(); close(done) }()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return errors.New("等待同步工作器退出超时")
	}
}
