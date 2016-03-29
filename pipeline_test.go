package pipeline
// +tag linux

import (
    "bytes"
    "fmt"
    "log"
    "testing"
    "os"
    "os/exec"
    "runtime/pprof"
    "time"
)

func TestExec(t *testing.T) {
    cat := exec.Command("cat") // Useless use of cat to test inner links
    wc := exec.Command("wc", "--char")
    sort := exec.Command("tee", "/dev/stderr")

    inbuff := &bytes.Buffer{}
    outbuff := &bytes.Buffer{}
    reportbuff := &bytes.Buffer{}

    fmt.Fprintln(inbuff, "Hello, world")

    done := make(chan bool)
    go func() {
        time.Sleep(500 * time.Millisecond)
        select {
        case <-done:
            return
        default:
            prof := pprof.Lookup("goroutine")
            err := prof.WriteTo(os.Stderr, 1)
            if err != nil {
                log.Println(err)
            }
            log.Fatal("timeout")
        }
    }()

    pl := New(inbuff, outbuff, reportbuff)
    pl.Chain(cat, wc, sort)

    pl.Exec()

    var count int
    n, err := fmt.Fscan(outbuff, &count)

    if err != nil {
        t.Error("Error after", n, "items")
    }

    if count != 13 {
        t.Error("Expected 13 but received", count)
    }

    var report int
    n, err = fmt.Fscan(reportbuff, report)
    if err != nil {
        t.Error("Error after", n, "items")
    }

    if report != 13 {
        t.Error("Expected 13 but received", report)
    }
    done<- true
}