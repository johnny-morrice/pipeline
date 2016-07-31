package pipeline

import (
    "errors"
    "bytes"
    "io"
    "os/exec"
    "sync"
)

type Pipeline struct {
    input io.Reader
    output io.Writer
    err io.Writer
    tasks []*exec.Cmd
}

type linkage struct {
    errs []io.ReadCloser
}

// Exec runs the pipeline: stdout from process n will be fed to process n + 1.
func (pl *Pipeline) Exec() error {
    link, linkerr := pl.linkPipes()

    if linkerr != nil {
        return linkerr
    }

    starterr := pl.start()
    if starterr != nil {
        return starterr
    }

    setuperr := pl.copyPipes(link)
    if setuperr != nil {
        return setuperr
    }

    waiterr := pl.wait()
    if waiterr != nil {
        return waiterr
    }

    return nil
}

func (pl *Pipeline) start() error {
    errs := []error{}
    // Launch all subprocesses
    for _, t := range pl.tasks {
        err := t.Start()
        if err != nil {
            errs = append(errs, err)
        }
    }

    return joinErrs(errs)
}

func (pl *Pipeline) wait() error {
    errs := []error{}

    for _, t := range pl.tasks {
        procerr := t.Wait()
        if procerr != nil {
            errs = append(errs, procerr)
        }
    }

    return joinErrs(errs)
}

func (pl *Pipeline) copyPipes(link linkage) error {
    // Sync pipe copy operations
    wg := sync.WaitGroup{}
    // Error buffer
    taskcnt := len(pl.tasks)
    exceptions := make([]chan error, 2 * taskcnt)

    // Copy errors to cache
    errcache := make([]chan io.Reader, taskcnt)
    wg.Add(taskcnt)
    for i, t := range pl.tasks {
        buffch := make(chan io.Reader)
        errcache[i] = buffch

        errch := make(chan error)
        exceptions[i] = errch

        reportpipe := link.errs[i]
        go func (task *exec.Cmd) {
            buff := &bytes.Buffer{}
            _, cpyerr := io.Copy(buff, reportpipe)
            go func() {
                buffch<- buff
            }()

            go func() {
                if cpyerr != nil {
                    errch<- cpyerr
                }
                close(errch)
            }()
            wg.Done()
        }(t)
    }

    // Copy errors out
    wg.Add(1)
    offset := taskcnt
    for i := offset; i < len(exceptions); i++ {
        errch := make(chan error)
        exceptions[i] = errch
    }
    go func() {
        for i, buffch := range errcache {
            errch := exceptions[offset + i]
            buff := <-buffch
            _, cpyerr := io.Copy(pl.err, buff)
            go func() {
                if cpyerr != nil {
                    errch<- cpyerr
                }
                close(errch)
            }()
        }
        wg.Done()
    }()

    // Wait for goroutines
    wg.Wait()

    errs := []error{}
    for _, repch := range exceptions {
        err, readok := <-repch
        if readok {
            errs = append(errs, err)
        }
    }

    return joinErrs(errs)
}

func (pl *Pipeline) linkPipes() (linkage, error) {
    taskcnt := len(pl.tasks)
    errpipes := make([]io.ReadCloser, taskcnt)
    link := linkage{}

    // Setup first task
    first := pl.tasks[0]
    prevout, preverr, perr0 := pipes(first)
    if perr0 != nil {
        return link, perr0
    }

    first.Stdin = pl.input

    errpipes[0] = preverr

    // Setup intervening tasks
    for i := 1; i < taskcnt - 1; i++ {
        curr := pl.tasks[i]
        currout, currerr, perr := pipes(curr)
        if perr != nil {
            return link, perr
        }
        errpipes[i] = currerr
        curr.Stdin = prevout
        prevout, preverr = currout, currerr
    }

    // Setup last task
    lastdex := taskcnt - 1
    last := pl.tasks[lastdex]
    last.Stdout = pl.output
    if taskcnt > 1 {
            last.Stdin = prevout
            lasterr, perrN := last.StderrPipe()
            if perrN != nil {
                return link, perrN
            }
            errpipes[lastdex] = lasterr
    }

    link.errs = errpipes

    return link, nil
}

// Chain adds tasks to the pipeline.
func (pl *Pipeline) Chain(cmds ...*exec.Cmd) {
    pl.tasks = append(pl.tasks, cmds...)
}

// New creates a new pipeline
func New(stdin io.Reader, stdout io.Writer, stderr io.Writer) *Pipeline {
    pl := &Pipeline{}
    pl.input = stdin
    pl.output = stdout
    pl.err = stderr
    pl.tasks = []*exec.Cmd{}
    return pl
}

func pipes(task *exec.Cmd) (io.ReadCloser, io.ReadCloser, error) {
    outp, outerr := task.StdoutPipe()
    if outerr != nil {
        return nil, nil, outerr
    }

    errp, errerr := task.StderrPipe()
    if errerr != nil {
        return nil, nil, errerr
    }

    return outp, errp, nil
}

func joinErrs(errs []error) error {
    buffer := bytes.Buffer{}

    if len(errs) > 0 {
        err0 := errs[0]
        buffer.WriteString(err0.Error())
        for _, err := range errs[1:] {
            buffer.WriteString("\n" + err.Error())
        }
        return errors.New(buffer.String())
    } else {
        return nil
    }
}
