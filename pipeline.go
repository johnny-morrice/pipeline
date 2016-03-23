package pchain

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
    // Verbose is a switch controlling error reporting.  If false, then stderr reports are
    // limited to those processes prior to and including the first process that failed. If true,
    // then all stderr reports will be output
    Verbose bool
}

type linkage struct {
    w io.Writer
    r io.Reader
    errs []io.Reader
}

type taskerr struct {
    errpipe io.Reader
    index int
}

// Exec runs the pipeline: stdout from process n will be fed to process n + 1.
func (pl *Pipeline) Exec() error {
    link, linkerr := pl.linkPipes()

    if linkerr != nil {
        return linkerr
    }

    taskerrch, setuperr := pl.copyPipes(link)
    if setuperr != nil {
        return setuperr
    }

    errlim, runerr := pl.runTasks(link)
    if runerr != nil {
        return runerr
    }

    return pl.reportErrors(taskerrch, errlim)
}

func (pl *Pipeline) reportErrors(taskerrch <-chan taskerr, errlim int) error {
    errs := []error{}
    // Copy stderr with respect to order and verbosity
    for te := range taskerrch {
        if te.index <= errlim {
            _, reporterr := io.Copy(pl.err, te.errpipe)
            if reporterr != nil {
                errs = append(errs, reporterr)
            }
        }
    }

    return joinErrs(errs)
}

func (pl *Pipeline) runTasks(link linkage) (int, error) {
    errs := []error{}

    // Launch all subprocesses
    for _, t := range pl.tasks {
        t.Start()
    }

    // Wait for subprocess to stop
    var errlim int
    if pl.Verbose {
        errlim = len(pl.tasks)
    }
    bad := false
    for i, t := range pl.tasks {
        procerr := t.Wait()
        if procerr != nil {
            if !bad && !pl.Verbose {
                errlim = i
            }
            errs = append(errs, procerr)
        }
    }

    return errlim, joinErrs(errs)
}

func (pl *Pipeline) copyPipes(link linkage) (<-chan taskerr, error) {
    errch := make(chan error)

    // Sync pipe copy operations
    wg := sync.WaitGroup{}

    // Input copy
    wg.Add(1)
    go func() {
        _, inerr := io.Copy(link.w, pl.input)
        if inerr != nil {
            errch<- inerr
        }
        wg.Done()
    }()

    // Output copy
    wg.Add(1)
    go func(){
        _, outerr := io.Copy(pl.output, link.r)
        if outerr != nil {
            errch<- outerr
        }
        wg.Done()
    }()

    // Error copy
    taskerrch := make(chan taskerr)
    taskcnt := len(pl.tasks)

    errg := sync.WaitGroup{}
    wg.Add(taskcnt)
    errg.Add(taskcnt)

    for i, t := range pl.tasks {
        go func (index int, task *exec.Cmd) {
            read, write := io.Pipe()
            _, cpyerr := io.Copy(write, link.errs[index])
            go func() {
                te := taskerr{}
                te.errpipe = read
                te.index = index
                taskerrch<- te
                errg.Done()
            }()

            if cpyerr != nil {
                errch<- cpyerr
            }
            wg.Done()
        }(i, t)
    }

    // Wait for goroutines
    go func() {
        errg.Wait()
        close(taskerrch)
    }()

    go func() {
        wg.Wait()
        close(errch)
    }()

    return taskerrch, pumpErrs(errch)
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

func pumpErrs(errch <-chan error) error {
    errs := []error{}

    for err := range errch {
        errs = append(errs, err)
    }

    return joinErrs(errs)
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
