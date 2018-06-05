package handler

import (
	"net/http"
	"strings"
	"sync"

	"github.com/go-martini/martini"
	"github.com/martini-contrib/render"
	"github.com/xuebing1110/location/amap"
	//"github.com/xuebing1110/rtbus/api"
	"github.com/xuebing1110/rtbus/pkg/httputil"
	"github.com/xuebing1110/rtbus/pkg/rtbus"
)

type NearestBusStation struct {
	City           string                `json:"city"`
	CityName       string                `json:"cityname"`
	StationName    string                `json:"sn"`
	LineNos        []string              `json:"linenos"`
	SupoortLineNos []string              `json:"supportlns"`
	Lines          []*BusLineDirOverview `json:"lines"`
}

type BusLineDirOverview struct {
	LineNo     string              `json:"lineno"`
	Direction  int                 `json:"linedir"`
	AnotherDir string              `json:"another_dir"`
	StartSn    string              `json:"startsn,omitempty"`
	EndSn      string              `json:"endsn,omitempty"`
	NextSn     string              `json:"nextsn"`
	Price      string              `json:"price,omitempty"`
	SnNum      int                 `json:"stationsNum,omitempty"`
	SnIndex    int                 `json:"stationIndex"`
	FirstTime  string              `json:"firstTime,omitempty"`
	LastTime   string              `json:"lastTime,omitempty"`
	Buses      []*rtbus.RunningBus `json:"buses"`
	IsSupport  bool                `json:"issupport"`
}

var (
	amapClient *amap.Client
	AMAP_KEY   = `b3abf03fa1e83992727f0625a918fe73`
)

func init() {
	amapClient = amap.NewClient(AMAP_KEY)
	amapClient.HttpClient = httputil.DEFAULT_HTTP_CLIENT
}

func BusLineOverview(params martini.Params, r render.Render) {
	city := params["city"]
	linenos := params["linenos"]
	sn := params["station"]

	var lock sync.Mutex
	var wg sync.WaitGroup

	var lineno_array = strings.Split(linenos, ",")
	var bldos = make([]*BusLineDirOverview, len(lineno_array))
	for index, lineno := range lineno_array {
		wg.Add(1)
		go func(index int, lineno string) {
			defer wg.Done()
			bldo := GetBusLineDirOverview(city, lineno, sn, true)

			lock.Lock()
			defer lock.Unlock()
			bldos[index] = bldo
		}(index, lineno)
	}
	wg.Wait()

	r.JSON(200,
		&Response{
			0,
			"OK",
			bldos,
		},
	)

}

func BusLineSuggest(params martini.Params, r render.Render, httpreq *http.Request) {
	lat := params["lat"]
	lon := params["lon"]

	httpreq.ParseForm()
	lazy := httpreq.Form.Get("lazy")

	//nearest bus line
	req := amap.NewInputtipsRequest(amapClient, "公交车站").
		SetCityLimit(true).
		SetLatLon(lat, lon)
	resp, err := req.Do()
	if err != nil {
		r.JSON(
			502,
			&Response{502, err.Error(), nil},
		)
		return
	}

	var city = req.City
	var cityname = req.CityName

	tip_len := len(resp.Tips)
	if tip_len > 6 {
		tip_len = 6
	}

	var globalWg sync.WaitGroup
	nbss := make([]*NearestBusStation, tip_len)
	for sni, tip := range resp.Tips {
		if sni >= tip_len {
			break
		}

		globalWg.Add(1)
		go func(sni int, tip *amap.Tip) {
			defer globalWg.Done()
			sn := strings.TrimRight(tip.Name, "(公交站)")
			nbs := &NearestBusStation{
				City:        city,
				CityName:    cityname,
				StationName: sn,
			}

			//lazy load
			var loadBus bool = true
			if lazy != "" && sni > 0 {
				loadBus = false
			}

			var wg sync.WaitGroup
			var linenames = strings.Split(tip.Address, ";")
			var linenos = make([]string, len(linenames))
			var bldos = make([]*BusLineDirOverview, len(linenames))
			for index, linename := range linenames {
				// if strings.Index(linename, "停运") >= 0 {
				// 	continue
				// }

				//lineno
				lineno_1 := strings.SplitN(linename, `/`, -1)
				lineno := strings.TrimRight(lineno_1[0], "专线车")
				lineno = strings.TrimRight(lineno, "线")
				lineno = strings.TrimRight(lineno, "路")
				lineno = strings.Replace(lineno, "路内环", "内", 1)
				lineno = strings.Replace(lineno, "路外环", "外", 1)
				lineno = strings.Replace(lineno, "路大站快车", "大站", 1)
				lineno = strings.Replace(lineno, "路大站快", "大站", 1)
				lineno = strings.Replace(lineno, "路快线", "快线", 1)
				lineno = strings.Replace(lineno, "路", "", 1)

				linenos[index] = lineno
				//logger.Info("found %s => %s", linename, lineno)

				wg.Add(1)
				go func(index int, lineno string) {
					defer wg.Done()
					bldo := GetBusLineDirOverview(city, lineno, sn, loadBus)
					bldos[index] = bldo
				}(index, lineno)
			}

			wg.Wait()

			//the count of support line
			var supportlns = make([]string, 0)
			for _, bldo := range bldos {
				if bldo != nil && bldo.IsSupport {
					supportlns = append(supportlns, bldo.LineNo)
				}
			}

			nbs.Lines = bldos
			nbs.LineNos = linenos
			nbs.SupoortLineNos = supportlns
			nbss[sni] = nbs
		}(sni, tip)
	}
	globalWg.Wait()

	r.JSON(200,
		&Response{
			0,
			"OK",
			nbss,
		},
	)
}

func GetBusLineDirOverview(city, lineno, station string, loadBus bool) (bldo *BusLineDirOverview) {
	bldo = &BusLineDirOverview{
		LineNo:    lineno,
		IsSupport: false,
	}

	dirid := "0"
	bdi, err := RTBusClient.GetBusLineDir(city, lineno, dirid)
	if err != nil {
		logger.Warn("%v", err)
		return
	}

	bldo.IsSupport = true
	bldo.Direction = bdi.Direction
	bldo.StartSn = bdi.StartSn
	bldo.EndSn = bdi.EndSn
	bldo.Price = bdi.Price
	bldo.SnNum = bdi.SnNum
	bldo.FirstTime = bdi.FirstTime
	bldo.LastTime = bdi.LastTime

	if len(bdi.OtherDirIDs) > 0 {
		bldo.AnotherDir = bdi.OtherDirIDs[0]
	}

	//get station index
	for _, s := range bdi.Stations {
		if s.Name == station {
			bldo.SnIndex = s.No
			break
		}
	}
	if bldo.SnIndex > 0 {
		// end station
		if bldo.SnIndex == bdi.SnNum {
			bldo.NextSn = bdi.EndSn
		} else {
			bldo.NextSn = bdi.Stations[bldo.SnIndex].Name
		}
	}

	//get running buses
	if loadBus {
		//logger.Info("getrt %s %s %s", city, lineno, dirid)
		rbuses, err := RTBusClient.GetRunningBus(city, lineno, dirid)
		//logger.Info("getrt %s %s %s over!", city, lineno, dirid)
		if err != nil {
			logger.Warn("%v", err)
		} else {
			rbs := make([]*rtbus.RunningBus, 0)
			for _, rbus := range rbuses {
				if rbus.No <= bldo.SnIndex || bldo.SnIndex == 0 {
					rbs = append(rbs, rbus)
				} else {
					break
				}
			}

			//reserve
			bldo.Buses = make([]*rtbus.RunningBus, 0)
			for i := len(rbs) - 1; i >= 0; i-- {
				bldo.Buses = append(bldo.Buses, rbs[i])
			}
		}
	}

	return bldo
}
