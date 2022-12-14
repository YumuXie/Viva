package gol

import (
	"fmt"
	"log"
	"net/rpc"
	"os"
	"strconv"
	"time"
	stubs "uk.ac.bris.cs/gameoflife/gol-stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	keyPresses <-chan rune
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

func makeMatrix(height, width int) [][]uint8 {
	matrix := make([][]uint8, height)
	for i := range matrix {
		matrix[i] = make([]uint8, width)
	}
	return matrix
}

func calculateAliveCells(p Params, world [][]byte) []util.Cell {
	sliceOfCells := make([]util.Cell, 0)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if world[y][x] == 255 {
				newCell := util.Cell{X: x, Y: y}
				sliceOfCells = append(sliceOfCells, newCell)
			}
		}
	}
	return sliceOfCells
}

func makeCall(client *rpc.Client, world [][]uint8, p Params, channel chan [][]uint8) { // p is gol.params type
	request := stubs.Request{World: world, Params: stubs.Params(p)} // Transferring world and params simultaneously
	response := new(stubs.Response)                                 // Pointer
	client.Call(stubs.GoLWorkerHandler, request, response)          // Client calls server
	channel <- response.World                                       // Transferring updated world to distributor function
}

//func makeKeyCall(client *rpc.Client, key rune, p Params, intChannel chan int, worldChannel chan [][]uint8) {
//	request := stubs.KeyRequest{Key: key, Params: stubs.Params(p)}
//	response := new(stubs.KeyResponse)
//	client.Call(stubs.GoLKeyWorkerHandler, request, response)
//	intChannel <- response.Turn
//	worldChannel <- response.World
//}

//func makeNewCall(client *rpc.Client, b bool, turnChannel chan int, cellChannel chan int) {
//	request := stubs.NewRequest{B: b}                         // Transferring world and params simultaneously
//	response := new(stubs.NewResponse)                        // Pointer
//	client.Call(stubs.GoLNewWorkerHandler, request, response) // Client calls server
//	fmt.Println("Response Turn in newCall: ", response.Turn)
//	fmt.Println("Response Cell in newCall: ", response.Cells)
//	turnChannel <- response.Turn
//	fmt.Println("Turn sent!")
//	cellChannel <- response.Cells
//	fmt.Println("Cells sent!")
//}

//func size(p Params, world [][]uint8) int {
//	output := 0
//	for y := 0; y < p.ImageHeight; y++ {
//		for x := 0; x < p.ImageWidth; x++ {
//			if world[y][x] == 255 || world[y][x] == 0 {
//				output++
//			}
//		}
//	}
//	return output
//}

// distributor divides the work between workers and interacts with other goroutines.
// Local controller will be responsible for IO and capturing keypresses.
// As a client on a local machine.
func distributor(p Params, c distributorChannels) {

	// TODO: Create a 2D slice to store the world.
	world := makeMatrix(p.ImageHeight, p.ImageWidth)

	final := makeMatrix(p.ImageHeight, p.ImageWidth)

	channel := make(chan [][]uint8, p.ImageHeight*p.ImageWidth) // Shared channel to transfer updated world

	//var mu sync.Mutex

	// Unbuffered will cause bug
	// Because this system is not 'face-to-face', it requires time to transfer, the data cannot reach immediately
	// If unbuffered, it will get stuck
	// turnChannel := make(chan int, p.Turns)
	// cellChannel := make(chan int, size(p, world))

	//intChannel := make(chan int, p.Turns)
	//worldChannel := make(chan [][]uint8, p.ImageHeight*p.ImageWidth)

	// turn := 0

	// Getting data from IO, and save it to world
	c.ioCommand <- ioInput
	c.ioFilename <- strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			back := <-c.ioInput
			world[y][x] = back
		}
	}

	//server := flag.String("server", "127.0.0.1:8030", "IP:port string to connect to as server")
	//flag.Parse() // Execute the command-line parsing
	//client, _ := rpc.Dial("tcp", *server)
	//defer client.Close()

	// TODO: Make client
	server := "35.172.135.125:8030"
	client, err := rpc.Dial("tcp", server)
	if err != nil { // error happens
		log.Fatal("dialing:", err)
	}
	defer client.Close()

	done := make(chan bool) // Judge the ticker in client should be stopped or not

	// Ticker
	// A light thread on the local controller(client) that calls the AWS node(server) and gets count every 2 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		for {
			select {
			case d := <-done:
				if d == true {
					ticker.Stop()
				}
			case <-ticker.C:
				//makeNewCall(client, true, turnChannel, cellChannel)
				request := stubs.NewRequest{B: true}                      // Transferring world and params simultaneously
				response := new(stubs.NewResponse)                        // Pointer
				client.Call(stubs.GoLNewWorkerHandler, request, response) // Client calls server
				//fmt.Println("Turn: ", response.Turn)
				//fmt.Println("Cell: ", response.Cells)
				c.events <- AliveCellsCount{response.Turn, response.Cells}
			}
		}
	}()

	// Key presses
	go func() {
		for {
			key := <-c.keyPresses
			if key == 's' {
				// The controller should generate a PGM file with the current state of the board
				request := stubs.KeyRequest{Key: 's'}
				response := new(stubs.KeyResponse)
				client.Call(stubs.GoLKeyWorkerHandler, request, response)
				// err = client.Call(stubs.GoLKeyWorkerHandler, request, response)
				//if err != nil {
				//  os.Exit(10)
				//	fmt.Println(err)
				//}
				c.ioCommand <- ioOutput
				c.ioFilename <- strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(response.Turn)
				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						c.ioOutput <- response.World[y][x] // Sending back
					}
				}
				c.events <- ImageOutputComplete{response.Turn, strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(response.Turn)}
			} else if key == 'q' {
				// close the controller client program without causing an error on the GoL server
				// A new controller should be able to take over interaction with the GoL engine
				// Note that you are free to define the nature of how a new controller can take over interaction
				// Most likely the state will be reset
				// If you do manage to continue with the previous world this would be considered an extension and a form of fault tolerance
				request := stubs.KeyRequest{Key: 'q'}
				response := new(stubs.KeyResponse)
				client.Call(stubs.GoLKeyWorkerHandler, request, response)
				// Killing client only
				done <- true
				//c.ioCommand <- ioCheckIdle
				//<-c.ioIdle
				//c.events <- StateChange{response.Turn, Quitting}
				//close(c.events)
				os.Exit(0)
			} else if key == 'k' {
				// All components of the distributed system are shut down cleanly, and the system outputs a PGM image of the latest state
				request := stubs.KeyRequest{Key: 's'}
				response := new(stubs.KeyResponse)
				client.Call(stubs.GoLKeyWorkerHandler, request, response)
				c.ioCommand <- ioOutput
				c.ioFilename <- strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(response.Turn)
				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						c.ioOutput <- response.World[y][x] // Sending back
					}
				}
				c.events <- ImageOutputComplete{response.Turn, strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(response.Turn)}
				KRequest := stubs.KeyRequest{Key: 'k'}
				KResponse := new(stubs.KeyResponse)
				client.Call(stubs.GoLKeyWorkerHandler, KRequest, KResponse)
				// Killing
				done <- true
				os.Exit(0)
			} else if key == 'p' {
				// pause the processing on the AWS node and have the controller print the current turn that is being processed
				// If p is pressed again resume the processing and have the controller print "Continuing"
				request := stubs.KeyRequest{Key: 'p'}
				response := new(stubs.KeyResponse)
				client.Call(stubs.GoLKeyWorkerHandler, request, response)
				c.events <- StateChange{response.Turn, Paused}
				fmt.Println("Current turn is: ", response.Turn)
				if <-c.keyPresses == 'p' { // If p is pressed again resume the processing and print "Continuing"
					newRequest := stubs.BestRequest{Key: 'p'}
					newResponse := new(stubs.BestResponse)
					client.Call(stubs.GoLBestWorkerHandler, newRequest, newResponse)
					c.events <- StateChange{newResponse.Turn, Executing}
					fmt.Println("Continuing")
				}
			}
		}
	}()

	// Sending world to client, then client sends data to GoL engine to process
	//fmt.Println("Called: ", world)
	makeCall(client, world, p, channel)

	temp := <-channel // Getting updated world back and save it to temp
	world = temp      // Update 'final' world

	// Getting data back from client, the data should be processed by engine already
	// Eventually, final = world
	final = world

	// TODO: Report the final state using FinalTurnCompleteEvent.
	c.events <- FinalTurnComplete{p.Turns, calculateAliveCells(p, final)}

	// IO receives the ioOutput command and outputs the state of the board after all turns have completed
	c.ioCommand <- ioOutput
	c.ioFilename <- strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.Turns)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- final[y][x] // Sending an array of bytes to IO
		}
	}
	c.events <- ImageOutputComplete{p.Turns, strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.Turns)}

	// Final turn is completed, close ticker gracefully
	done <- true
	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{p.Turns, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
