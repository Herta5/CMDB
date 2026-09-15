// 本文件验证后端测试入口对临时数据库的隔离、失败传递和清理责任。
package database

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestBackendTestEntrypointOwnsEphemeralDatabase 使用进程替身模拟 Docker 边界，验证测试进程实际获得的环境与生命周期结果。
func TestBackendTestEntrypointOwnsEphemeralDatabase(t *testing.T) {
	for _, scenario := range []struct {
		name                               string
		startupExit, testExit, cleanupExit int
		port                               string
		interrupt                          bool
		wantSuccess, wantTests             bool
	}{
		{name: "测试阻塞期间中断仍清理", interrupt: true, port: "127.0.0.1:49152", wantTests: true},
		{name: "测试成功后清理", port: "127.0.0.1:49152", wantSuccess: true, wantTests: true},
		{name: "测试失败仍清理", port: "127.0.0.1:49152", testExit: 23, wantTests: true},
		{name: "启动失败仍清理", startupExit: 17},
		{name: "拒绝无效发布端口", port: "0.0.0.0:5432"},
		{name: "清理失败不可通过", port: "127.0.0.1:49152", cleanupExit: 19, wantTests: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			directory := t.TempDir()
			writeExecutable(t, filepath.Join(directory, "docker"), `#!/bin/sh
set -eu
[ "$1" = compose ] || exit 90
shift
if [ "$1" = version ]; then exit 0; fi
[ "$1" = --project-name ] || exit 91
project="$2"
shift 2
[ "$1" = --file ] || exit 92
[ -f "$2" ] || exit 93
shift 2
case "$1" in
 up)
  [ "$project" != "production-sentinel" ] || exit 94
  printf '%s' "$project" > "$TEST_STATE/owner"
  touch "$TEST_STATE/database"
  case " $* " in *' --wait '*) ;; *) exit 95 ;; esac
  exit "$TEST_STARTUP_EXIT" ;;
 port)
  printf '%s\n' "$TEST_PORT" ;;
 down)
  [ "$project" = "$(cat "$TEST_STATE/owner")" ] || exit 96
  if [ "$TEST_CLEANUP_EXIT" != 0 ]; then exit "$TEST_CLEANUP_EXIT"; fi
  rm "$TEST_STATE/database" ;;
 *) exit 97 ;;
esac
`)
			writeExecutable(t, filepath.Join(directory, "go"), `#!/bin/sh
set -eu
[ "$*" = 'test -tags=postgres -count=1 ./...' ] || exit 80
[ "$(basename "$PWD")" = backend ] || exit 81
[ "$CMDB_POSTGRES_TEST_DSN" = 'postgres://postgres:cmdb-integration-only@127.0.0.1:49152/postgres?sslmode=disable' ] || exit 82
[ -f "$TEST_STATE/database" ] || exit 83
touch "$TEST_STATE/tests-executed"
if [ "$TEST_INTERRUPT" = true ]; then
 # 模拟不协作的测试进程，中断不能无限等待其自行退出。
 trap '' TERM
 echo $$ > "$TEST_STATE/child-pid"
 exec sleep 30
fi
exit "$TEST_EXIT"
`)
			command := exec.Command("bash", "../../../../scripts/test-backend.sh")
			command.Env = append(os.Environ(),
				"PATH="+directory+":"+os.Getenv("PATH"), "TEST_STATE="+directory,
				"TEST_STARTUP_EXIT="+strconv.Itoa(scenario.startupExit), "TEST_EXIT="+strconv.Itoa(scenario.testExit),
				"TEST_INTERRUPT="+strconv.FormatBool(scenario.interrupt),
				"TEST_CLEANUP_EXIT="+strconv.Itoa(scenario.cleanupExit), "TEST_PORT="+scenario.port,
				"COMPOSE_PROJECT_NAME=production-sentinel", "CMDB_POSTGRES_TEST_DSN=不可使用外部数据库")
			var output []byte
			var err error
			if scenario.interrupt {
				var childPID int
				var captured strings.Builder
				command.Stdout, command.Stderr = &captured, &captured
				if startErr := command.Start(); startErr != nil {
					t.Fatal("启动中断场景失败")
				}
				done := make(chan error, 1)
				go func() { done <- command.Wait() }()
				defer func() {
					if childPID > 0 {
						_ = syscall.Kill(childPID, syscall.SIGKILL)
					}
					_ = command.Process.Kill()
				}()
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					raw, readErr := os.ReadFile(filepath.Join(directory, "child-pid"))
					if readErr == nil {
						childPID, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
						if childPID > 0 {
							break
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				if childPID == 0 {
					t.Fatal("模拟测试子进程未开始阻塞")
				}
				if signalErr := command.Process.Signal(syscall.SIGTERM); signalErr != nil {
					t.Fatal("发送中断信号失败")
				}
				select {
				case err = <-done:
				case <-time.After(time.Second):
					t.Fatal("父进程中断必须及时结束阻塞测试并清理数据库")
				}
				output = []byte(captured.String())
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 143 {
					t.Fatalf("TERM 中断必须返回143：%v", err)
				}
				if syscall.Kill(childPID, 0) == nil {
					t.Fatal("中断入口不得留下仍在运行的测试子进程")
				}
				childPID = 0
			} else {
				output, err = command.CombinedOutput()
			}
			if (err == nil) != scenario.wantSuccess {
				t.Fatalf("入口退出状态不符合预期：%v，输出：%s", err, output)
			}
			if scenario.testExit != 0 {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != scenario.testExit {
					t.Fatalf("入口必须保留测试失败退出码：%v", err)
				}
			}
			if _, err := os.Stat(filepath.Join(directory, "owner")); err != nil {
				t.Fatal("入口必须启动本次隔离数据库")
			}
			_, testErr := os.Stat(filepath.Join(directory, "tests-executed"))
			if (testErr == nil) != scenario.wantTests {
				t.Fatalf("只有数据库健康且端口可信时才能运行测试：%s", output)
			}
			if _, err := os.Stat(filepath.Join(directory, "database")); scenario.cleanupExit == 0 && !os.IsNotExist(err) {
				t.Fatal("入口退出必须清理本次临时数据库")
			}
			if strings.Contains(string(output), "不可使用外部数据库") {
				t.Fatal("入口不得输出调用方的数据库连接")
			}
		})
	}
}

// writeExecutable 为入口测试创建进程替身，避免测试连接真实部署环境。
func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal("创建测试进程替身失败")
	}
}
