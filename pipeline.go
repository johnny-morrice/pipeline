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
    w io.Writer
    r io.Reader
    errs []io.Reader
    errcache []io.Reader
}

// Exec runs the pipeline: stdout from process n will be fed to process n + 1.
func (pl *Pipeline) Exec() error {
    link, linkerr := pl.linkPipes()

    if linkerr != nil {
        return linkerr
    }

    setuperr := pl.copyPipes(link)
    if setuperr != nil {
        return setuperr
    }

    runerr := pl.runTasks(link)
    if runerr != nil {
        return runerr
    }

    return nil
}

func (pl *Pipeline) runTasks(link linkage) error {
    errs := []error{}

    // Launch all subprocesses
    for _, t := range pl.tasks {
        t.Start()
    }

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
    exceptions := make([]chan error, (2 * taskcnt) + 2)

    // Input copy
    wg.Add(1)
    inerrch := make(chan error)
    exceptions[0] = inerrch
    go func() {
        _, inerr := io.Copy(link.w, pl.input)
        go func() {
            if inerr != nil {
                inerrch<- inerr
            }
            close(inerrch)
        }()
        wg.Done()
    }()

    // Output copy
    wg.Add(1)
    outerrch := make(chan error)
    exceptions[1] = outerrch
    go func(){
        _, outerr := io.Copy(pl.output, link.r)
        go func() {
            if outerr != nil {
                outerrch<- outerr
            }
            close(outerrch)
        }()

        wg.Done()
    }()

    // Copy errors to cache
    errcache := make([]io.Reader, taskcnt)
    wg.Add(taskcnt)
    for i, t := range pl.tasks {
        read, write := io.Pipe()
        errcache[i] = read

        errch := make(chan error)
        exceptions[i + 2] = errch

        reportpipe := link.errs[i]
        go func (task *exec.Cmd) {
            _, cpyerr := io.Copy(write, reportpipe)

            go func() {
                if cpyerr != nil {
                    errch<- cpyerr
                }
                close(errch)
            }()
            wg.Done()
        }(t)
    }

    // Wait for goroutines
    wg.Wait()

    // Copy errors out
    offset := taskcnt + 2
    for i, reportpipe := range link.errcache {
        errch := make(chan error)
        exceptions[offset + i] = errch
        _, cpyerr := io.Copy(pl.err, reportpipe)
        go func() {
            if cpyerr != nil {
                errch<- cpyerr
            }
            close(errch)
        }()
    }

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
    errpipes := make([]io.Reader, taskcnt)
    link := linkage{}

    // Setup first task
    first := pl.tasks[0]
    prevout, preverr, perr0 := pipes(first)
    if perr0 != nil {
        return link, perr0
    }

    firstin, ferr := first.StdinPipe()
    if ferr != nil {
        return link, ferr
    }

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
    last := taskcnt - 1
    lastout, lasterr, perrN := pipes(pl.tasks[last])
    if perrN != nil {
        return link, nil
    }
    errpipes[last] = lasterr

    link.w = firstin
    link.r = lastout
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