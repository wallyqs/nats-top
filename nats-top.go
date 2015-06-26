// Copyright (c) 2015 NATS Messaging System
package main

import (
        "bufio"
        "flag"
        "fmt"
        "log"
        "os"
        "os/signal"
        "sort"
        "sync"
        "syscall"
        "time"

        "github.com/nats-io/gnatsd/server"
        . "github.com/nats-io/nats-top/util"
        ui "github.com/gizak/termui"
)

const natsTopVersion = "1.0.0"

var (
        host   = flag.String("s", "127.0.0.1", "The nats server host")
        port   = flag.Int("m", 8333, "The nats server monitoring port")
        conns  = flag.Int("n", 1024, "Num of connections")
        delay  = flag.Int("d", 1, "Delay in monitoring interval in seconds")
        sortBy = flag.String("sort", "cid", "Value for which to sort by the connections")
        showVersion = flag.Bool("v", false, "Show nats-top version")
        uiStyle = flag.String("ui", "simple", "Select UI style")
)

func usage() {
        log.Fatalf("Usage: nats-top [-s server] [-m monitor_port] [-n num_connections] [-d delay_secs] [--sort by]\n")
}

func init() {
        log.SetFlags(0)
        flag.Usage = usage
        flag.Parse()
}

func main() {

        if *showVersion {
                log.Printf("nats-top v%s", natsTopVersion)
                os.Exit(0)
        }

        opts := make(map[string]interface{})
        opts["host"] = *host
        opts["port"] = *port
        opts["conns"] = *conns
        opts["delay"] = *delay
        opts["header"] = ""

        if opts["host"] == nil || opts["port"] == nil {
                log.Fatalf("Please specify the monitoring port for NATS.")
                usage()
        }

        sortingOptions := map[string]bool{
                "cid":        true,
                "subs":       true,
                "pending":    true,
                "msgs_to":    true,
                "msgs_from":  true,
                "bytes_to":   true,
                "bytes_from": true,
        }

        if !sortingOptions[*sortBy] {
                log.Printf("nats-top: not a valid option to sort by: %s\n", *sortBy)
                log.Println("Sort by options: ")
                for k, _ := range sortingOptions {
                        log.Printf("         %s\n", k)
                }
                usage()
        }
        opts["sort"] = *sortBy

        // Smoke test the server once before starting
        _, err := Request("/varz", opts)
        if err != nil {
                log.Fatalf("ERROR: %v", err)
                os.Exit(1)
        }

        switch *uiStyle {
        case "simple":
                go StartSimpleUI(opts)
        case "dashboard", "graphs":
                varzch := make(chan *server.Varz)
                connzch := make(chan *server.Connz)

                go GetStats(opts, varzch, connzch)
                StartDashboardUI(opts, varzch, connzch)
        default:
		sigch := make(chan os.Signal)
		keych := make(chan string)
		waitingSortOption := false
		signal.Notify(sigch, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

                go StartSimpleUI(opts)
                go listenKeyboard(keych)
                for {
                        select {
                        case <-sigch:
                                cleanExit()

                        case keys := <-keych:
                                if !waitingSortOption && keys == "o\n" {
                                        opts["header"] = fmt.Sprintf("\033[1;1H\033[6;1Hsort by [%s]: ", opts["sort"])
                                        waitingSortOption = true
                                        continue
                                }
                                if !waitingSortOption && keys == "q\n" {
                                        cleanExit()
                                }

                                if waitingSortOption {
                                        switch keys {
                                        case "cid\n":
                                                opts["sort"] = "cid"
                                        case "subs\n":
                                                opts["sort"] = "subs"
                                        case "pending\n":
                                                opts["sort"] = "pending"
                                        case "msgs_to\n":
                                                opts["sort"] = "msgs_to"
                                        case "msgs_from\n":
                                                opts["sort"] = "msgs_from"
                                        case "bytes_to\n":
                                                opts["sort"] = "bytes_to"
                                        case "bytes_from\n":
                                                opts["sort"] = "bytes_from"
                                        }
                                        waitingSortOption = false
                                        opts["header"] = ""
                                }
                        }
                }
        }
}

func clearScreen() {
        fmt.Print("\033[2J\033[1;1H\033[?25l")
}

func cleanExit() {
        clearScreen()

        // Show cursor once again
        fmt.Print("\033[?25h")
        os.Exit(0)
}

func listenKeyboard(keych chan string) {
        for {
                reader := bufio.NewReader(os.Stdin)
                keys, _ := reader.ReadString('\n')
                keych <- keys
        }
}

func StartDashboardUI(opts map[string]interface{}, varzch chan *server.Varz, connzch chan *server.Connz) {

        err := ui.Init()
        if err != nil {
                panic(err)
       	}
        defer ui.Close()

	//      === Current Layout ===
	//
        // ....cpu.........  ...mem.........
        // .              .  .             .
        // ....conss.......  .             .
        // .              .  .             .
        // .              .  .             .
        // ................  ...............
	//
        // ..in msgs/sec...  ..in bytes/sec.
        // .              .  .             .
        // .              .  .             .
        // .              .  .             .
        // .              .  .             .
        // ................  ...............
	//
        // ..out msgs/sec..  .out bytes/sec.
        // .              .  .             .
        // .              .  .             .
        // .              .  .             .
        // .              .  .             .
        // ................  ...............
	//

	cpuChart := ui.NewGauge()
        cpuChart.Border.Label = "Cpu: "
        cpuChart.Height = ui.TermHeight() / 7
	cpuChart.BarColor = ui.ColorGreen
	cpuChart.PercentColor = ui.ColorBlue

	connsChart := ui.NewLineChart()
        connsChart.Border.Label = "Connections: "
        connsChart.Height = ui.TermHeight() / 5
	connsChart.Mode = "dot"
        connsChart.AxesColor = ui.ColorWhite
        connsChart.LineColor = ui.ColorYellow | ui.AttrBold
	
        memChart := ui.NewLineChart()
        memChart.Border.Label = "Mem: "
        memChart.Height = ui.TermHeight() / 3
	memChart.Mode = "dot"
        memChart.AxesColor = ui.ColorWhite
        memChart.LineColor = ui.ColorYellow | ui.AttrBold
	
        inMsgsChart := ui.NewLineChart()
        inMsgsChart.Border.Label = "In Msgs/Sec: "
        inMsgsChart.Height = ui.TermHeight() / 3
	inMsgsChart.Mode = "dot"	
        inMsgsChart.AxesColor = ui.ColorWhite
        inMsgsChart.LineColor = ui.ColorYellow | ui.AttrBold

        inBytesChart := ui.NewLineChart()
        inBytesChart.Border.Label = "In Bytes/Sec: "
        inBytesChart.Height = ui.TermHeight() / 3
	inBytesChart.Mode = "dot"
        inBytesChart.AxesColor = ui.ColorWhite
        inBytesChart.LineColor = ui.ColorYellow | ui.AttrBold

        outMsgsChart := ui.NewLineChart()
        outMsgsChart.Border.Label = "Out Msgs/Sec: "
        outMsgsChart.Height = ui.TermHeight() / 3
	outMsgsChart.Mode = "dot"
        outMsgsChart.AxesColor = ui.ColorWhite
        outMsgsChart.LineColor = ui.ColorYellow | ui.AttrBold

        outBytesChart := ui.NewLineChart()
        outBytesChart.Border.Label = "Out Bytes/Sec: "
        outBytesChart.Height = ui.TermHeight() / 3
	outBytesChart.Mode = "dot"	
        outBytesChart.AxesColor = ui.ColorWhite
        outBytesChart.LineColor = ui.ColorYellow | ui.AttrBold
	
        // build layout
        ui.Body.AddRows(
                ui.NewRow(
			ui.NewCol(6, 0, cpuChart, connsChart),
			ui.NewCol(6, 0, memChart),
		),
                ui.NewRow(
			ui.NewCol(6, 0, inMsgsChart),
			ui.NewCol(6, 0, inBytesChart)),
                ui.NewRow(
			ui.NewCol(6, 0, outMsgsChart),
			ui.NewCol(6, 0, outBytesChart)),
	)

        // calculate layout
        ui.Body.Align()

	// Get delay value
	var delayVal int
	var ok bool
	if delayVal, ok = opts["delay"].(int); !ok {
		log.Fatalf("error: could not use %s as a refreshing interval", opts["delay"])
	}

        done := make(chan bool)
        redraw := make(chan bool)

        update := func() {

		var pollTime time.Time

		var inMsgsDelta int64
		var outMsgsDelta int64
		var inBytesDelta int64
		var outBytesDelta int64

		var inMsgsLastVal int64
		var outMsgsLastVal int64
		var inBytesLastVal int64
		var outBytesLastVal int64

		var inMsgsRate float64
		var outMsgsRate float64
		var inBytesRate float64
		var outBytesRate float64

		first := true
		pollTime = time.Now()
		
                for {
			varz := <- varzch
			connz := <- connzch

			// Periodic snapshot to get per sec metrics
			inMsgsVal := varz.InMsgs
			outMsgsVal := varz.OutMsgs
			inBytesVal := varz.InBytes
			outBytesVal := varz.OutBytes

			inMsgsDelta = inMsgsVal - inMsgsLastVal
			outMsgsDelta = outMsgsVal - outMsgsLastVal
			inBytesDelta = inBytesVal - inBytesLastVal
			outBytesDelta = outBytesVal - outBytesLastVal

			inMsgsLastVal = inMsgsVal
			outMsgsLastVal = outMsgsVal
			inBytesLastVal = inBytesVal
			outBytesLastVal = outBytesVal

			now := time.Now()
			tdelta := now.Sub(pollTime)
			pollTime = now

			// Calculate rates but the first time
			if !first {
			        inMsgsRate = float64(inMsgsDelta) / tdelta.Seconds()
			        outMsgsRate = float64(outMsgsDelta) / tdelta.Seconds()
			        inBytesRate = float64(inBytesDelta) / tdelta.Seconds()
			        outBytesRate = float64(outBytesDelta) / tdelta.Seconds()
			}

			cpuChart.Border.Label = fmt.Sprintf("CPU: %.1f%% ", varz.CPU)
			cpuChart.Percent = int(varz.CPU)

			// Sparklines
			memChart.Border.Label = fmt.Sprintf("Memory: %s (%d)", Psize(varz.Mem), len(memChart.Data))
			memChart.Data = append(memChart.Data, float64(varz.Mem / 1024 / 1024))
			if len(memChart.Data) > 100 {
				memChart.Data = memChart.Data[1:100]
			}

			connsChart.Border.Label = fmt.Sprintf("Connections: %d/%d ", connz.NumConns, varz.Options.MaxConn)
			connsChart.Data = append(connsChart.Data, float64(connz.NumConns))
			if len(connsChart.Data) > 100 {
				connsChart.Data = connsChart.Data[1:100]
			}
			// connsChart.Data = append(connsChart.Data, float64(connz.NumConns))
			// if len(connsChart.Data) > 100 {
			// 	connsChart.Data = connsChart.Data[1:100]
			// }

			inMsgsChart.Border.Label = fmt.Sprintf("In: Msgs/Sec: %.1f ", inMsgsRate)
			inMsgsChart.Data = append(inMsgsChart.Data, inMsgsRate)
			if len(inMsgsChart.Data) > 100 {
				inMsgsChart.Data = inMsgsChart.Data[1:100]
			}
			
			inBytesChart.Border.Label = fmt.Sprintf("In: Bytes/Sec: %s ", Psize(int64(inBytesRate)))
			inBytesChart.Data = append(inBytesChart.Data, inBytesRate)
			if len(inBytesChart.Data) > 100 {
				inBytesChart.Data = inBytesChart.Data[1:100]
			}

			outMsgsChart.Border.Label = fmt.Sprintf("Out: Msgs/Sec: %.1f ", outMsgsRate)
			outMsgsChart.Data = append(outMsgsChart.Data, outMsgsRate)
			if len(outMsgsChart.Data) > 100 {
				outMsgsChart.Data = outMsgsChart.Data[1:100]
			}

			outBytesChart.Border.Label = fmt.Sprintf("Out: Bytes/Sec: %s ", Psize(int64(outBytesRate)))
			outBytesChart.Data = append(outBytesChart.Data, outBytesRate)
			if len(outBytesChart.Data) > 100 {
				outBytesChart.Data = outBytesChart.Data[1:100]
			}

			if first {
				first = false
			}

			time.Sleep(time.Duration(delayVal) * time.Second)
                        redraw <- true
                }
                done <- true
        }

        evt := ui.EventCh()

        ui.Render(ui.Body)
        go update()

        for {
                select {
                case e := <-evt:
                        if e.Type == ui.EventKey && e.Ch == 'q' {
                                return
                        }
                        if e.Type == ui.EventResize {
                                ui.Body.Width = ui.TermWidth()

				// Refresh size of boxes accordingly
				cpuChart.Height = ui.TermHeight() / 7
				connsChart.Height = ui.TermHeight() / 5
				memChart.Height = ui.TermHeight() / 3
				inMsgsChart.Height = ui.TermHeight() / 3
				outMsgsChart.Height = ui.TermHeight() / 3
				inBytesChart.Height = ui.TermHeight() / 3
				outBytesChart.Height = ui.TermHeight() / 3

				ui.Body.Align()
                                go func() { redraw <- true }()
                        }
                case <-done:
                        return
                case <-redraw:
                        ui.Render(ui.Body)
                }
        }
}

// Will pass the values to a varz and connz channels
// StatsLoop
func GetStats(opts map[string]interface{}, varzch chan *server.Varz, connzch chan *server.Connz) {
        for {
                // Getting the stats --------------------------------------------------
                wg := &sync.WaitGroup{}
                wg.Add(2)

                // Periodically poll for the varz, connz and routez
                var varz *server.Varz
                go func() {
                        var err error
                        defer wg.Done()

                        result, err := Request("/varz", opts)
                        if err != nil {
                                log.Fatalf("Could not get /varz: %v", err)
                        }

                        if varzVal, ok := result.(*server.Varz); ok {
                                varz = varzVal
                        }
                }()

                var connz *server.Connz
                go func() {
                        var err error
                        defer wg.Done()

                        result, err := Request("/connz", opts)
                        if err != nil {
                                log.Fatalf("Could not get /connz: %v", err)
                        }

                        if connzVal, ok := result.(*server.Connz); ok {
                                connz = connzVal
                        }
                }()
                wg.Wait()

                varzch <- varz
                connzch <- connz

                // FIXME: para := generateParagraph(varz, Conn)
                //
                // cpu := varz.CPU
                // numConns := connz.NumConns
                // memVal := varz.Mem

                // // Periodic snapshot to get per sec metrics
                // inMsgsVal := varz.InMsgs
                // outMsgsVal := varz.OutMsgs
                // inBytesVal := varz.InBytes
                // outBytesVal := varz.OutBytes

                // inMsgsDelta = inMsgsVal - inMsgsLastVal
                // outMsgsDelta = outMsgsVal - outMsgsLastVal
                // inBytesDelta = inBytesVal - inBytesLastVal
                // outBytesDelta = outBytesVal - outBytesLastVal

                // inMsgsLastVal = inMsgsVal
                // outMsgsLastVal = outMsgsVal
                // inBytesLastVal = inBytesVal
                // outBytesLastVal = outBytesVal

                // now := time.Now()
                // tdelta := now.Sub(pollTime)
                // pollTime = now

                // // Calculate rates but the first time
                // if !first {
                //         inMsgsRate = float64(inMsgsDelta) / tdelta.Seconds()
                //         outMsgsRate = float64(outMsgsDelta) / tdelta.Seconds()
                //         inBytesRate = float64(inBytesDelta) / tdelta.Seconds()
                //         outBytesRate = float64(outBytesDelta) / tdelta.Seconds()
                // }

                // mem := Psize(memVal)
                // inMsgs := Psize(inMsgsVal)
                // outMsgs := Psize(outMsgsVal)
                // inBytes := Psize(inBytesVal)
                // outBytes := Psize(outBytesVal)

                // info := "\nServer:\n  Load: CPU: %.1f%%  Memory: %s\n"
                // info += "  In:   Msgs: %s  Bytes: %s  Msgs/Sec: %.1f  Bytes/Sec: %.1f\n"
                // info += "  Out:  Msgs: %s  Bytes: %s  Msgs/Sec: %.1f  Bytes/Sec: %.1f"

                // text := fmt.Sprintf(info, cpu, mem,
                //         inMsgs, inBytes, inMsgsRate, inBytesRate,
                //         outMsgs, outBytes, outMsgsRate, outBytesRate)
                // text += fmt.Sprintf("\n\nConnections: %d\n", numConns)

                // connHeader := "  %-20s %-8s %-6s  %-10s  %-10s  %-10s  %-10s  %-10s  %-7s  %-7s\n"

                // connRows := fmt.Sprintf(connHeader, "HOST", "CID", "SUBS", "PENDING",
                //         "MSGS_TO", "MSGS_FROM", "BYTES_TO", "BYTES_FROM",
                //         "LANG", "VERSION")
                // text += connRows
                // connValues := "  %-20s %-8d %-6d  %-10d  %-10s  %-10s  %-10s  %-10s  %-7s  %-7s\n"

                // switch opts["sort"] {
                // case "cid":
                //         sort.Sort(ByCid(connz.Conns))
                // case "subs":
                //         sort.Sort(sort.Reverse(BySubs(connz.Conns)))
                // case "pending":
                //         sort.Sort(sort.Reverse(ByPending(connz.Conns)))
                // case "msgs_to":
                //         sort.Sort(sort.Reverse(ByMsgsTo(connz.Conns)))
                // case "msgs_from":
                //         sort.Sort(sort.Reverse(ByMsgsFrom(connz.Conns)))
                // case "bytes_to":
                //         sort.Sort(sort.Reverse(ByBytesTo(connz.Conns)))
                // case "bytes_from":
                //         sort.Sort(sort.Reverse(ByBytesFrom(connz.Conns)))
                // }

                // for _, conn := range connz.Conns {
                //         host := fmt.Sprintf("%s:%d", conn.IP, conn.Port)
                //         connLine := fmt.Sprintf(connValues, host, conn.Cid, conn.NumSubs, conn.Pending,
                //                 Psize(conn.OutMsgs), Psize(conn.InMsgs), Psize(conn.OutBytes), Psize(conn.InBytes),
                //                 conn.Lang, conn.Version)
                //         text += connLine
                // }
                // if first {
                //         first = false
                // }
                // -------------------------------------------------------------------------------
                // clearScreen()
                // fmt.Print(text)
                // Move cursor to sort by options position
                // fmt.Print(opts["header"])
                // Handled by UI now
                if val, ok := opts["delay"].(int); ok {
                        time.Sleep(time.Duration(val) * time.Second)
                        // clearScreen()
                } else {
                        log.Fatalf("error: could not use %s as a refreshing interval", opts["delay"])
                        break
                }
        }
}

func StartSimpleUI(opts map[string]interface{}) {
        var pollTime time.Time

        var inMsgsDelta int64
        var outMsgsDelta int64
        var inBytesDelta int64
        var outBytesDelta int64

        var inMsgsLastVal int64
        var outMsgsLastVal int64
        var inBytesLastVal int64
        var outBytesLastVal int64

        var inMsgsRate float64
        var outMsgsRate float64
        var inBytesRate float64
        var outBytesRate float64

        first := true
        pollTime = time.Now()
        for {
                wg := &sync.WaitGroup{}
                wg.Add(2)

                // Periodically poll for the varz, connz and routez
                var varz *server.Varz
                go func() {
                        var err error
                        defer wg.Done()

                        result, err := Request("/varz", opts)
                        if err != nil {
                                log.Fatalf("Could not get /varz: %v", err)
                        }

                        if varzVal, ok := result.(*server.Varz); ok {
                                varz = varzVal
                        }
                }()

                var connz *server.Connz
                go func() {
                        var err error
                        defer wg.Done()

                        result, err := Request("/connz", opts)
                        if err != nil {
                                log.Fatalf("Could not get /connz: %v", err)
                        }

                        if connzVal, ok := result.(*server.Connz); ok {
                                connz = connzVal
                        }
                }()
                wg.Wait()

                cpu := varz.CPU
                numConns := connz.NumConns
                memVal := varz.Mem

                // Periodic snapshot to get per sec metrics
                inMsgsVal := varz.InMsgs
                outMsgsVal := varz.OutMsgs
                inBytesVal := varz.InBytes
                outBytesVal := varz.OutBytes

                inMsgsDelta = inMsgsVal - inMsgsLastVal
                outMsgsDelta = outMsgsVal - outMsgsLastVal
                inBytesDelta = inBytesVal - inBytesLastVal
                outBytesDelta = outBytesVal - outBytesLastVal

                inMsgsLastVal = inMsgsVal
                outMsgsLastVal = outMsgsVal
                inBytesLastVal = inBytesVal
                outBytesLastVal = outBytesVal

                now := time.Now()
                tdelta := now.Sub(pollTime)
                pollTime = now

                // Calculate rates but the first time
                if !first {
                        inMsgsRate = float64(inMsgsDelta) / tdelta.Seconds()
                        outMsgsRate = float64(outMsgsDelta) / tdelta.Seconds()
                        inBytesRate = float64(inBytesDelta) / tdelta.Seconds()
                        outBytesRate = float64(outBytesDelta) / tdelta.Seconds()
                }

                mem := Psize(memVal)
                inMsgs := Psize(inMsgsVal)
                outMsgs := Psize(outMsgsVal)
                inBytes := Psize(inBytesVal)
                outBytes := Psize(outBytesVal)

                info := "\nServer:\n  Load: CPU: %.1f%%  Memory: %s\n"
                info += "  In:   Msgs: %s  Bytes: %s  Msgs/Sec: %.1f  Bytes/Sec: %.1f\n"
                info += "  Out:  Msgs: %s  Bytes: %s  Msgs/Sec: %.1f  Bytes/Sec: %.1f"

                text := fmt.Sprintf(info, cpu, mem,
                        inMsgs, inBytes, inMsgsRate, inBytesRate,
                        outMsgs, outBytes, outMsgsRate, outBytesRate)
                text += fmt.Sprintf("\n\nConnections: %d\n", numConns)

                connHeader := "  %-20s %-8s %-6s  %-10s  %-10s  %-10s  %-10s  %-10s  %-7s  %-7s\n"

                connRows := fmt.Sprintf(connHeader, "HOST", "CID", "SUBS", "PENDING",
                        "MSGS_TO", "MSGS_FROM", "BYTES_TO", "BYTES_FROM",
                        "LANG", "VERSION")
                text += connRows
                connValues := "  %-20s %-8d %-6d  %-10d  %-10s  %-10s  %-10s  %-10s  %-7s  %-7s\n"

                switch opts["sort"] {
                case "cid":
                        sort.Sort(ByCid(connz.Conns))
                case "subs":
                        sort.Sort(sort.Reverse(BySubs(connz.Conns)))
                case "pending":
                        sort.Sort(sort.Reverse(ByPending(connz.Conns)))
                case "msgs_to":
                        sort.Sort(sort.Reverse(ByMsgsTo(connz.Conns)))
                case "msgs_from":
                        sort.Sort(sort.Reverse(ByMsgsFrom(connz.Conns)))
                case "bytes_to":
                        sort.Sort(sort.Reverse(ByBytesTo(connz.Conns)))
                case "bytes_from":
                        sort.Sort(sort.Reverse(ByBytesFrom(connz.Conns)))
                }

                for _, conn := range connz.Conns {
                        host := fmt.Sprintf("%s:%d", conn.IP, conn.Port)
                        connLine := fmt.Sprintf(connValues, host, conn.Cid, conn.NumSubs, conn.Pending,
                                Psize(conn.OutMsgs), Psize(conn.InMsgs), Psize(conn.OutBytes), Psize(conn.InBytes),
                                conn.Lang, conn.Version)
                        text += connLine
                }
                clearScreen()
                fmt.Print(text)

                // Move cursor to sort by options position
                fmt.Print(opts["header"])

                if first {
                        first = false
                }

                if val, ok := opts["delay"].(int); ok {
                        time.Sleep(time.Duration(val) * time.Second)
                        clearScreen()
                } else {
                        log.Fatalf("error: could not use %s as a refreshing interval", opts["delay"])
                        break
                }
        }
}
