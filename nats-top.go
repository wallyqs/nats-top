// Copyright (c) 2015 NATS Messaging System
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	ui "github.com/gizak/termui"
	"github.com/nats-io/gnatsd/server"
	. "github.com/nats-io/nats-top/util"
)

const natsTopVersion = "1.0.0"

var (
	host        = flag.String("s", "127.0.0.1", "The nats server host")
	port        = flag.Int("m", 8333, "The nats server monitoring port")
	conns       = flag.Int("n", 1024, "Num of connections")
	delay       = flag.Int("d", 1, "Delay in monitoring interval in seconds")
	sortBy      = flag.String("sort", "cid", "Value for which to sort by the connections")
	showVersion = flag.Bool("v", false, "Show nats-top version")
	uiStyle     = flag.String("ui", "simple", "Select UI style")
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

	err = ui.Init()
	if err != nil {
		panic(err)
	}
	defer ui.Close()

	varzch := make(chan *server.Varz)
	connzch := make(chan *server.Connz)
	ratesch := make(chan map[string]float64)

	go GetStats(opts, varzch, connzch, ratesch)

	// Change into render functions?
	// dont pass the channel just the values
	switch *uiStyle {
	case "simple":
		StartRatesUI(opts, varzch, connzch, ratesch)
	case "dashboard", "graphs":
		StartDashboardUI(opts, varzch, connzch, ratesch)
	default:
		// keych := make(chan string)
		// waitingSortOption := false
		StartRatesUI(opts, varzch, connzch, ratesch)

		// for {
		//         select {
		//         case keys := <-keych:
		//                 if !waitingSortOption && keys == "o\n" {
		//                         opts["header"] = fmt.Sprintf("\033[1;1H\033[6;1Hsort by [%s]: ", opts["sort"])
		//                         waitingSortOption = true
		//                         continue
		//                 }
		//                 if !waitingSortOption && keys == "q\n" {
		//                         cleanExit()
		//                 }

		//                 if waitingSortOption {
		//                         switch keys {
		//                         case "cid\n":
		//                                 opts["sort"] = "cid"
		//                         case "subs\n":
		//                                 opts["sort"] = "subs"
		//                         case "pending\n":
		//                                 opts["sort"] = "pending"
		//                         case "msgs_to\n":
		//                                 opts["sort"] = "msgs_to"
		//                         case "msgs_from\n":
		//                                 opts["sort"] = "msgs_from"
		//                         case "bytes_to\n":
		//                                 opts["sort"] = "bytes_to"
		//                         case "bytes_from\n":
		//                                 opts["sort"] = "bytes_from"
		//                         }
		//                         waitingSortOption = false
		//                         opts["header"] = ""
		//                 }
		//         }
		// }
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

func StartDashboardUI(opts map[string]interface{}, varzch chan *server.Varz, connzch chan *server.Connz, ratesch chan map[string]float64) {

	// cpu and conns share the same space in the grid so handled differently
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
	connsChart.Data = []float64{0}

	// All other boxes of the same size
	boxHeight := ui.TermHeight() / 3

	memChart := ui.NewLineChart()
	memChart.Border.Label = "Memory: "
	memChart.Height = boxHeight
	memChart.Mode = "dot"
	memChart.AxesColor = ui.ColorWhite
	memChart.LineColor = ui.ColorYellow | ui.AttrBold
	memChart.Data = []float64{0.0}

	inMsgsChartLine := ui.Sparkline{}
	inMsgsChartLine.Height = boxHeight - boxHeight/7
	inMsgsChartLine.LineColor = ui.ColorCyan
	inMsgsChartLine.TitleColor = ui.ColorWhite
	inMsgsChartBox := ui.NewSparklines(inMsgsChartLine)
	inMsgsChartLine.Data = []int{0}
	inMsgsChartBox.Height = boxHeight
	inMsgsChartBox.Border.Label = "In Msgs/Sec: "

	inBytesChartLine := ui.Sparkline{}
	inBytesChartLine.Height = boxHeight - boxHeight/7
	inBytesChartLine.LineColor = ui.ColorCyan
	inBytesChartLine.TitleColor = ui.ColorWhite
	inBytesChartLine.Data = []int{0}
	inBytesChartBox := ui.NewSparklines(inBytesChartLine)
	inBytesChartBox.Height = boxHeight
	inBytesChartBox.Border.Label = "In Bytes/Sec: "

	outMsgsChartLine := ui.Sparkline{}
	outMsgsChartLine.Height = boxHeight - boxHeight/7
	outMsgsChartLine.LineColor = ui.ColorGreen
	outMsgsChartLine.TitleColor = ui.ColorWhite
	outMsgsChartLine.Data = []int{0}
	outMsgsChartBox := ui.NewSparklines(outMsgsChartLine)
	outMsgsChartBox.Height = boxHeight
	outMsgsChartBox.Border.Label = "Out Msgs/Sec: "

	outBytesChartLine := ui.Sparkline{}
	outBytesChartLine.Height = boxHeight - boxHeight/7
	outBytesChartLine.LineColor = ui.ColorGreen
	outBytesChartLine.TitleColor = ui.ColorWhite
	outBytesChartLine.Data = []int{0}
	outBytesChartBox := ui.NewSparklines(outBytesChartLine)
	outBytesChartBox.Height = boxHeight
	outBytesChartBox.Border.Label = "Out Bytes/Sec: "

	// ======== Current Layout =========
	//
	// ....cpu.........  ...mem.........
	// .              .  .             .
	// .              .  .             .
	// ....conns.......  .             .
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
	ui.Body.AddRows(
		ui.NewRow(
			ui.NewCol(6, 0, cpuChart, connsChart),
			ui.NewCol(6, 0, memChart),
		),
		ui.NewRow(
			ui.NewCol(6, 0, inMsgsChartBox),
			ui.NewCol(6, 0, inBytesChartBox)),
		ui.NewRow(
			ui.NewCol(6, 0, outMsgsChartBox),
			ui.NewCol(6, 0, outBytesChartBox)),
	)
	ui.Body.Align()

	done := make(chan bool)
	redraw := make(chan bool)

	update := func() {

		for {
			varz := <-varzch
			connz := <-connzch
			rates := <-ratesch

			inMsgsRate := rates["inMsgsRate"]
			outMsgsRate := rates["outMsgsRate"]
			inBytesRate := rates["inBytesRate"]
			outBytesRate := rates["outBytesRate"]

			cpuChart.Border.Label = fmt.Sprintf("CPU: %.1f%% ", varz.CPU)
			cpuChart.Percent = int(varz.CPU)

			connsChart.Border.Label = fmt.Sprintf("Connections: %d/%d ", connz.NumConns, varz.Options.MaxConn)
			connsChart.Data = append(connsChart.Data, float64(connz.NumConns))
			if len(connsChart.Data) > 150 {
				connsChart.Data = connsChart.Data[1:150]
			}

			memChart.Border.Label = fmt.Sprintf("Memory: %s", Psize(varz.Mem))
			memChart.Data = append(memChart.Data, float64(varz.Mem/1024/1024))
			if len(memChart.Data) > 150 {
				memChart.Data = memChart.Data[1:150]
			}

			inMsgsChartBox.Border.Label = fmt.Sprintf("In: Msgs/Sec: %.1f ", inMsgsRate)
			inMsgsChartBox.Lines[0].Data = append(inMsgsChartBox.Lines[0].Data, int(inMsgsRate))
			if len(inMsgsChartBox.Lines[0].Data) > 150 {
				inMsgsChartBox.Lines[0].Data = inMsgsChartBox.Lines[0].Data[1:150]
			}

			inBytesChartBox.Border.Label = fmt.Sprintf("In: Bytes/Sec: %s ", Psize(int64(inBytesRate)))
			inBytesChartBox.Lines[0].Data = append(inBytesChartBox.Lines[0].Data, int(inBytesRate))
			if len(inBytesChartBox.Lines[0].Data) > 150 {
				inBytesChartBox.Lines[0].Data = inBytesChartBox.Lines[0].Data[1:150]
			}

			outMsgsChartBox.Border.Label = fmt.Sprintf("Out: Msgs/Sec: %.1f ", outMsgsRate)
			outMsgsChartBox.Lines[0].Data = append(outMsgsChartBox.Lines[0].Data, int(outMsgsRate))
			if len(outMsgsChartBox.Lines[0].Data) > 150 {
				outMsgsChartBox.Lines[0].Data = outMsgsChartBox.Lines[0].Data[1:150]
			}

			outBytesChartBox.Border.Label = fmt.Sprintf("Out: Bytes/Sec: %s ", Psize(int64(outBytesRate)))
			outBytesChartBox.Lines[0].Data = append(outBytesChartBox.Lines[0].Data, int(outBytesRate))
			if len(outBytesChartBox.Lines[0].Data) > 150 {
				outBytesChartBox.Lines[0].Data = outBytesChartBox.Lines[0].Data[1:150]
			}

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

				boxHeight := ui.TermHeight() / 3
				lineHeight := boxHeight - boxHeight/7

				memChart.Height = boxHeight

				inMsgsChartBox.Height = boxHeight
				inMsgsChartBox.Lines[0].Height = lineHeight

				outMsgsChartBox.Height = boxHeight
				outMsgsChartBox.Lines[0].Height = lineHeight

				inBytesChartBox.Height = boxHeight
				inBytesChartBox.Lines[0].Height = lineHeight

				outBytesChartBox.Height = boxHeight
				outBytesChartBox.Lines[0].Height = lineHeight

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

// Will pass the values to a varz and connz channels, what about sending the rates too?
func GetStats(opts map[string]interface{}, varzch chan *server.Varz, connzch chan *server.Connz, ratesch chan map[string]float64) {

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

		rates := map[string]float64{
			"inMsgsRate":   inMsgsRate,
			"outMsgsRate":  outMsgsRate,
			"inBytesRate":  inBytesRate,
			"outBytesRate": outBytesRate,
		}

		varzch <- varz
		connzch <- connz
		ratesch <- rates

		if first {
			first = false
		}

		// -------------------------------------------------------------------------------
		// Move cursor to sort by options position
		// fmt.Print(opts["header"])
		// Handled by UI now

		// Note that delay defines the sampling rate as well
		if val, ok := opts["delay"].(int); ok {
			time.Sleep(time.Duration(val) * time.Second)
		} else {
			log.Fatalf("error: could not use %s as a refreshing interval", opts["delay"])
			break
		}
	}
}

func StartRatesUI(opts map[string]interface{}, varzch chan *server.Varz, connzch chan *server.Connz, ratesch chan map[string]float64) {

	par0 := ui.NewPar("Borderless Text")
	par0.Height = ui.TermHeight()
	par0.Width = ui.TermWidth()
	par0.HasBorder = false

	done := make(chan bool)
	redraw := make(chan bool)

	update := func() {
		for {
			varz := <-varzch
			connz := <-connzch
			rates := <-ratesch

			cpu := varz.CPU
			numConns := connz.NumConns
			memVal := varz.Mem
			inMsgsVal := varz.InMsgs
			outMsgsVal := varz.OutMsgs
			inBytesVal := varz.InBytes
			outBytesVal := varz.OutBytes

			inMsgsRate := rates["inMsgsRate"]
			outMsgsRate := rates["outMsgsRate"]
			inBytesRate := rates["inBytesRate"]
			outBytesRate := rates["outBytesRate"]

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
			par0.Text = text

			redraw <- true
		}
		done <- true
	}

	evt := ui.EventCh()
	ui.Render(par0)
	go update()

	for {
		select {
		case e := <-evt:
			if e.Type == ui.EventKey && e.Ch == 'q' {
				return
			}
			if e.Type == ui.EventResize {
				go func() { redraw <- true }()
			}
		case <-done:
			return
		case <-redraw:
			ui.Render(par0)
		}
	}
}

func StartSimpleUI(opts map[string]interface{}, varzch chan *server.Varz, connzch chan *server.Connz, ratesch chan map[string]float64) {
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
