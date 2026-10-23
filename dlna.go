package main

import (
	"encoding/xml"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const dlnaUUID = "uuid:76656c61-0000-4000-8000-000000000001"

// StartDLNA runs a minimal UPnP AV ContentDirectory server plus SSDP responder.
func StartDLNA(cfg *Config, st *Store) {
	port := cfg.DlnaPort
	if port == 0 {
		port = 8772
	}
	base := func(r *http.Request) string {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == "" || host == "0.0.0.0" {
			host = firstLANIP()
		}
		return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rootDesc.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		fmt.Fprintf(w, `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
 <specVersion><major>1</major><minor>0</minor></specVersion>
 <device>
  <deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>
  <friendlyName>Vela</friendlyName>
  <manufacturer>Vela</manufacturer>
  <modelName>Vela</modelName>
  <UDN>%s</UDN>
  <serviceList><service>
   <serviceType>urn:schemas-upnp-org:service:ContentDirectory:1</serviceType>
   <serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId>
   <SCPDURL>/ContentDirectory/scpd.xml</SCPDURL>
   <controlURL>/ContentDirectory/control</controlURL>
   <eventSubURL>/ContentDirectory/event</eventSubURL>
  </service></serviceList>
 </device>
</root>`, dlnaUUID)
	})

	mux.HandleFunc("/ContentDirectory/scpd.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		w.Write([]byte(`<?xml version="1.0"?><scpd xmlns="urn:schemas-upnp-org:service-1-0"><actionList>
<action><name>Browse</name><argumentList>
<argument><name>ObjectID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
<argument><name>BrowseFlag</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_BrowseFlag</relatedStateVariable></argument>
<argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
<argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
<argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
<argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
<argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
</argumentList></action></actionList></scpd>`))
	})

	mux.HandleFunc("/ContentDirectory/event", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("SID", "uuid:vela-event")
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/ContentDirectory/control", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0, 8192)
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil || len(body) > 1<<20 {
				break
			}
		}
		var env struct {
			Body struct {
				Browse struct {
					ObjectID       string `xml:"ObjectID"`
					BrowseFlag     string `xml:"BrowseFlag"`
					StartingIndex  int    `xml:"StartingIndex"`
					RequestedCount int    `xml:"RequestedCount"`
				} `xml:"Browse"`
			} `xml:"Body"`
		}
		_ = xml.Unmarshal(body, &env)
		b := env.Body.Browse
		start := b.StartingIndex
		if start < 0 {
			start = 0
		}
		count := b.RequestedCount
		if count <= 0 || count > 200 {
			count = 50
		}
		items, total, _ := st.List("", "", "name", 200, 0)
		if b.ObjectID != "0" && b.ObjectID != "" && b.ObjectID != "library" {
			items = nil
			total = 0
		}
		if start > len(items) {
			start = len(items)
		}
		end := start + count
		if end > len(items) {
			end = len(items)
		}
		var didl strings.Builder
		didl.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
		for _, m := range items[start:end] {
			ct := "video/mp4"
			fmt.Fprintf(&didl, `<item id="media:%s" parentID="0" restricted="1"><dc:title>%s</dc:title><upnp:class>object.item.videoItem</upnp:class><res protocolInfo="http-get:*:%s:*" size="%d">%s/media/%s</res></item>`,
				m.ID, escapeXML(m.Title), ct, m.Size, base(r), m.ID)
		}
		didl.WriteString(`</DIDL-Lite>`)
		result := escapeXML(didl.String())
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><Result>%s</Result><NumberReturned>%d</NumberReturned><TotalMatches>%d</TotalMatches><UpdateID>1</UpdateID></u:BrowseResponse></s:Body></s:Envelope>`,
			result, end-start, total)
	})

	mux.HandleFunc("/media/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/media/")
		m, err := st.Get(id)
		if err != nil || m.Trashed {
			http.NotFound(w, r)
			return
		}
		f, err := openFile(m.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("transferMode.dlna.org", "Streaming")
		w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_OP=01;DLNA.ORG_CI=0")
		http.ServeContent(w, r, m.Title, time.Now(), f)
	})

	log.Print("DLNA is read-only and has no authentication; enable it only on a trusted LAN")
	lan := firstLANIP()
	go func() {
		log.Printf("DLNA on %s:%d (LAN)", lan, port)
		if err := http.ListenAndServe(net.JoinHostPort(lan, strconv.Itoa(port)), mux); err != nil {
			log.Printf("dlna lan: %v", err)
		}
	}()
	go func() {
		// Loopback copy for local players and tests; not advertised over SSDP.
		_ = http.ListenAndServe("127.0.0.1:"+strconv.Itoa(port), mux)
	}()
	go ssdpResponder(port)
}

func ssdpResponder(port int) {
	addr, err := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if err != nil {
		return
	}
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		log.Printf("ssdp: %v", err)
		return
	}
	defer conn.Close()
	buf := make([]byte, 2048)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		msg := strings.ToUpper(string(buf[:n]))
		if !strings.HasPrefix(msg, "M-SEARCH") {
			continue
		}
		if !strings.Contains(msg, "SSDP:ALL") && !strings.Contains(msg, "CONTENTDIRECTORY") && !strings.Contains(msg, "MEDIASERVER") {
			continue
		}
		ip := firstLANIP()
		loc := fmt.Sprintf("http://%s:%d/rootDesc.xml", ip, port)
		resp := "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nEXT:\r\nST: urn:schemas-upnp-org:device:MediaServer:1\r\nUSN: " + dlnaUUID + "::urn:schemas-upnp-org:device:MediaServer:1\r\nLOCATION: " + loc + "\r\nSERVER: Vela/1 UPnP/1.1\r\n\r\n"
		_, _ = conn.WriteToUDP([]byte(resp), src)
	}
}

func firstLANIP() string {
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func escapeXML(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func openFile(path string) (*os.File, error) { return os.Open(path) }
