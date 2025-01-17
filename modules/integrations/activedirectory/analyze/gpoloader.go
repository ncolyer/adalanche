package analyze

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/lkarlslund/adalanche/modules/engine"
	"github.com/lkarlslund/adalanche/modules/integrations/activedirectory"
	"github.com/lkarlslund/adalanche/modules/util"
	"github.com/rs/zerolog/log"
)

var (
	gposource = engine.AttributeValueString("Group Policy dumps")
	GLoader   = engine.AddLoader(func() engine.Loader { return (&GPOLoader{}) })
)

type GPOLoader struct {
	importmutex      sync.Mutex
	done             sync.WaitGroup
	dco              map[string]*engine.Objects
	gpofiletoprocess chan string
}

func (ld *GPOLoader) Name() string {
	return gposource.String()
}

func (ld *GPOLoader) Init() error {
	ld.dco = make(map[string]*engine.Objects)
	ld.gpofiletoprocess = make(chan string, 8192)

	// GPO objects
	for i := 0; i < runtime.NumCPU(); i++ {
		ld.done.Add(1)
		go func() {
			for path := range ld.gpofiletoprocess {
				raw, err := ioutil.ReadFile(path)
				if err != nil {
					log.Warn().Msgf("Problem reading data from GPO JSON file %v: %v", path, err)
					continue
				}

				var ginfo activedirectory.GPOdump
				err = json.Unmarshal(raw, &ginfo)
				if err != nil {
					log.Warn().Msgf("Problem unmarshalling data from JSON file %v: %v", path, err)
					continue
				}

				thisao := ld.getShard(path)

				netbios := ginfo.DomainNetbios
				if netbios == "" {
					// Fallback to extracting from the domain DN
					netbios = util.ExtractNetbiosFromBase(ginfo.DomainDN)
				}
				if netbios == "" {
					// Fallback to using path
					parts := strings.Split(ginfo.Path, "\\")

					sysvol := -1
					for i, part := range parts {
						if strings.EqualFold(part, "sysvol") {
							sysvol = i
							break
						}
					}
					if sysvol != -1 && len(parts) > sysvol+2 && strings.EqualFold(parts[sysvol+2], "policies") {
						netbios, _, _ = strings.Cut(parts[sysvol+1], ".")
					}
					if netbios != "" {
						thisao.AddDefaultFlex(
							engine.UniqueSource, engine.AttributeValueString(netbios),
						)
					}
				}

				err = ImportGPOInfo(ginfo, thisao)
				if err != nil {
					log.Warn().Msgf("Problem importing GPO: %v", err)
					continue
				}
			}
			ld.done.Done()
		}()
	}
	return nil
}

func (ld *GPOLoader) getShard(path string) *engine.Objects {
	shard := filepath.Dir(path)

	lookupshard := shard

	var ao *engine.Objects
	ld.importmutex.Lock()
	ao = ld.dco[lookupshard]
	if ao == nil {
		ao = engine.NewLoaderObjects(ld)

		ao.SetThreadsafe(true)
		ld.dco[lookupshard] = ao
	}
	ld.importmutex.Unlock()
	return ao
}

func (ld *GPOLoader) Load(path string, cb engine.ProgressCallbackFunc) error {
	if strings.HasSuffix(path, ".gpodata.json") {
		ld.gpofiletoprocess <- path
		return nil
	}
	return engine.ErrUninterested
}

func (ld *GPOLoader) Close() ([]*engine.Objects, error) {
	close(ld.gpofiletoprocess)
	ld.done.Wait()

	var aos []*engine.Objects
	for _, ao := range ld.dco {
		aos = append(aos, ao)
		ao.SetThreadsafe(false)
	}

	ld.dco = nil
	return aos, nil
}
