package gomr

import (
	"log"
	"runtime"
	"sync"
)

// Architect and MapReduce Job with the following number of mappers and
// reducers. Return to the user a channel for intputing their data
func RunLocal(nMap, nRed int, j Job) (inMap []chan interface{},
	outRed chan interface{}) {

	inMap = make([]chan interface{}, nMap)
	inPar := make([]chan interface{}, nMap)
	inRed := make([]chan interface{}, nRed)
	outRed = make(chan interface{}, CHANBUF)

	var wgPar, wgRed sync.WaitGroup
	wgPar.Add(nMap)
	wgRed.Add(nRed)

	for i := 0; i < nRed; i++ {
		inRed[i] = make(chan interface{}, CHANBUF)
		go j.Reduce(inRed[i], outRed, &wgRed)
	}

	for i := 0; i < nMap; i++ {
		inMap[i] = make(chan interface{}, CHANBUF)
		inPar[i] = make(chan interface{}, CHANBUF)
		go j.Map(inMap[i], inPar[i])
		go j.Partition(inPar[i], inRed, &wgPar)
	}

	go func() {
		wgPar.Wait()
		for i := 0; i < nRed; i++ {
			close(inRed[i])
		}
		log.Println("Map done.")

		wgRed.Wait()
		close(outRed)
		log.Println("Reduce done.")
	}()

	return
}

type localShuffle struct {
	mux     sync.Mutex
	wg      sync.WaitGroup
	reducer Reducer
}

func (ls *localShuffle) shuffle(inRed, outRed chan interface{}, wg *sync.WaitGroup) {
	defer wg.Done()

	reducers := make(map[interface{}]chan interface{})

	for item := range inRed {
		key := item.(Keyer).Key()
		if _, ok := reducers[key]; !ok {
			ls.wg.Add(1)
			reducers[key] = make(chan interface{}, CHANBUF)
			go ls.reducer.Reduce(reducers[key], outRed, &ls.wg)
		}
		reducers[key] <- item
	}

	for _, v := range reducers {
		close(v)
	}

}

// Architect and MapReduce Job a dynamic number of mappers and reducers. Return
// to the user a channel for intputing their data
func RunLocalDynamic(m Mapper, p Partitioner, r Reducer) (inMap []chan interface{},
	outRed chan interface{}) {
	nCpu := runtime.NumCPU()

	inMap = make([]chan interface{}, nCpu)   // number of mappers == cpus
	inPar := make([]chan interface{}, nCpu)  // number of partitioners == nMap
	inRed := make([]chan interface{}, nCpu)  // number of reducer input == cpus
	outRed = make(chan interface{}, CHANBUF) // single output channel

	ls := localShuffle{ // reducer-generator
		reducer: r,
	}

	var wgPar, wgShuf sync.WaitGroup
	wgPar.Add(nCpu)
	wgShuf.Add(nCpu)

	for i := 0; i < nCpu; i++ {
		inRed[i] = make(chan interface{}, CHANBUF)
		go ls.shuffle(inRed[i], outRed, &wgShuf)
	}

	for i := 0; i < nCpu; i++ {
		inMap[i] = make(chan interface{}, CHANBUF)
		inPar[i] = make(chan interface{}, CHANBUF)
		go p.Partition(inPar[i], inRed, &wgPar)
		go m.Map(inMap[i], inPar[i])
	}

	go func() {
		wgPar.Wait()
		log.Println("Map and Partition done.")

		for i := 0; i < nCpu; i++ {
			close(inRed[i])
		}

		wgShuf.Wait()
		log.Println("Shuffle done.")

		ls.wg.Wait()
		log.Println("Reduce done.")

		close(outRed)
	}()

	return
}
