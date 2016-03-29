# Pipeline

A simple subprocess pipeline in Go.

## Aims

* Behaves basically as you would expect it to.
* Maximum throughput with minimal process blocking through concurrency.
* User sets up `exec.Cmd` structures for maximum flexibility.

## Short Example

From a test

    cat := exec.Command("cat") // Useless use of cat to test inner links
    wc := exec.Command("wc", "--char")
    sort := exec.Command("tee", "/dev/stderr")

    inbuff := &bytes.Buffer{}
    outbuff := &bytes.Buffer{}
    reportbuff := &bytes.Buffer{}

    fmt.Fprintln(inbuff, "Hello, world")

    var count int
    fmt.Fscan(outbuff, &count) // count == 13

    var report int
    fmt.Fscan(reportbuff, &report) // report == 13

## Status

Experimental.

## Credits

John Morrice

john@functorama.com

http://teoma.io/jmorrice

https://github.com/johnny-morrice
