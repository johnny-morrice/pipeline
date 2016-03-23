# Pipeline

An simple subprocess pipeline in Go.

## Aims

* Acts basically as you would expect it to.
* Maximum throughput through concurrency. 
* Configurably stderr verbosity.
* User sets up exec.Cmd structures for maximum flexibility.

## Short Example

    pl := pipeline.New(os.Stdin, os.Stdout, os.Stderr)
    pl.Verbose = true
    # Assuming you imported os/exec and made Cmd structures already
    pl.Chain(findCmd, lsCmd, grepCmd)
    # Chain takes variable numbers of arguments 
    pl.Chain(otherCommandList...)
    err := pl.Exec()

## Status

Just wrote this evening; test tomorrow.

## Credits

John Morrice

john@functorama.com

http://teoma.io/jmorrice

https://github.com/johnny-morrice
