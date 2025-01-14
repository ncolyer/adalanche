package engine

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	pwnAnalyzers = map[LoaderID][]PwnAnalyzer{}
)

func (l LoaderID) AddAnalyzers(pa ...PwnAnalyzer) {
	pwnAnalyzers[l] = append(pwnAnalyzers[l], pa...)
}

func Analyze(ao *Objects, cb ProgressCallbackFunc, l LoaderID) {
	objectslice := ao.Slice()
	max := len(objectslice) * len(pwnAnalyzers)
	div := max / 1000
	if div == 0 {
		// Division by Zero, no thanks
		div = 1
	}
	cb(0, max)

	timings := make([]time.Time, len(pwnAnalyzers[l]))

	ao.SetThreadsafe(true)

	starttime := time.Now()
	var wait sync.WaitGroup

	for i, an := range pwnAnalyzers[l] {
		wait.Add(1)
		go func(li int, lan PwnAnalyzer) {
			cur := 0
			for _, o := range objectslice {
				lan.ObjectAnalyzer(o, ao)
				cur++
				if cur%div == 0 {
					cb(-1000, -1)
				}
			}
			timings[li] = time.Now()
			cb(-cur%div, -1) // Add the final items to progressbar
			wait.Done()
		}(i, an)
	}
	wait.Wait()
	cb(max, max)
	endtime := time.Now()

	ao.SetThreadsafe(false)

	for i := range pwnAnalyzers[l] {
		log.Info().Msgf("Elapsed %vms for analysis %v", timings[i].Sub(starttime).Milliseconds(), pwnAnalyzers[l][i].Description)
	}
	log.Info().Msgf("Total elapsed %vms for analysis", endtime.Sub(starttime).Milliseconds())
}

type ProgressCallbackFunc func(progress int, totalprogress int)

type ProcessorFunc func(ao *Objects)

type ProcessPriority int

const (
	BeforeMergeLow ProcessPriority = iota
	BeforeMerge
	BeforeMergeHigh
	AfterMergeLow
	AfterMerge
	AfterMergeHigh
)

type ppfInfo struct {
	pf          ProcessorFunc
	description string
	priority    ProcessPriority
	loader      LoaderID
}

var registeredProcessors []ppfInfo

func (l LoaderID) AddProcessor(pf ProcessorFunc, description string, priority ProcessPriority) {
	registeredProcessors = append(registeredProcessors, ppfInfo{
		loader:      l,
		description: description,
		pf:          pf,
		priority:    priority,
	})
}

func Process(ao *Objects, cb ProgressCallbackFunc, l LoaderID, priority ProcessPriority) error {
	for _, processor := range registeredProcessors {
		if processor.loader == l && processor.priority == priority {
			if priority < AfterMergeLow {
				log.Info().Msgf("Preprocessing %v ...", processor.description)
			} else {
				log.Info().Msgf("Postprocessing %v ...", processor.description)
			}
			processor.pf(ao)
		}
	}
	return nil // FIXME
}
