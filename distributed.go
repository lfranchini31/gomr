package gomr

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"runtime"
	"sync"
)

type worker struct {
	config map[string]interface{}
	id     int
	role   int
	ncpu   int
	input  string
	job    Job
}

func (w *worker) runMapper() {
	nRed := len(w.config["reducers"].([]interface{}))
	inMap := make([]chan interface{}, w.ncpu)
	inPar := make([]chan interface{}, w.ncpu)
	inRed := make([]chan interface{}, nRed)

	var wgPar, wgShuf sync.WaitGroup
	wgPar.Add(w.ncpu)
	wgShuf.Add(nRed)
	defer wgShuf.Wait()

	for i := 0; i < nRed; i++ {
		inRed[i] = make(chan interface{}, CHANBUF)
		go w.shuffle(i, inRed[i], &wgShuf)
	}

	for i := 0; i < w.ncpu; i++ {
		inMap[i] = make(chan interface{}, CHANBUF)
		inPar[i] = make(chan interface{}, CHANBUF)
		go w.job.Partition(inPar[i], inRed, &wgPar)
		go w.job.Map(inMap[i], inPar[i])
	}

	go func() {
		wgPar.Wait()
		for i := 0; i < nRed; i++ {
			close(inRed[i])
		}
		log.Println("Map and partition done.")
	}()

	TextFileParallel(w.input, inMap)
}

func (w *worker) shuffle(i int, inRed chan interface{}, wg *sync.WaitGroup) {
	defer wg.Done()
	dst := w.config["reducers"].([]interface{})[i].(string)
	client := newClient(dst)
	defer client.close()

	for item := range inRed {
		client.transmit(item.([]byte))
	}
}

func (w *worker) runReducer() {
	server := newServer(
		w.config["reducers"].([]interface{})[w.id].(string),
		int(w.config["nmappers"].(float64)),
	)

	fromNet := server.serve()
	inRed := make([]chan interface{}, 10*w.ncpu)
	outRed := make(chan interface{}, CHANBUF)

	var wgPar, wgRed sync.WaitGroup
	wgPar.Add(1)
	wgRed.Add(len(inRed))

	for i := 0; i < len(inRed); i++ {
		inRed[i] = make(chan interface{}, CHANBUF)
		go w.job.Reduce(inRed[i], outRed, &wgRed)
	}

	go w.job.Partition(fromNet, inRed, &wgPar)

	go func() {
		wgPar.Wait()
		for _, ch := range inRed {
			close(ch)
		}

		wgRed.Wait()
		close(outRed)
	}()

	for item := range outRed {
		fmt.Println(item)
	}
}

// RunDistributed executes a Mapper or Reducer process in a distributed
// environment.
func RunDistributed(job Job) {
	w := worker{}
	ncpu := runtime.NumCPU()
	id := flag.Int("id", 0, "What is the reducer id of the worker?")
	role := flag.Int("role", MAPPER, "What is the role of this worker")
	input := flag.String("input", "input.txt", "Path to input file")
	configFn := flag.String("conf", "config.json", "Path to a config file for GoMR")
	flag.Parse()

	// open and parse configFn
	config := make(map[string]interface{})
	data, err := ioutil.ReadFile(*configFn)
	if err != nil {
		log.Fatal("Unable to read config file: ", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatal(err)
	}

	w.config = config
	w.ncpu = ncpu
	w.id = *id
	w.role = *role
	w.input = *input
	w.job = job

	log.Printf("%+v\n", w)

	switch *role {
	case MAPPER:
		w.runMapper()
	case REDUCER:
		w.runReducer()
	}
}