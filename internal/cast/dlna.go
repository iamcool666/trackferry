package cast

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	avTransport = "urn:schemas-upnp-org:service:AVTransport:1"
	rendering   = "urn:schemas-upnp-org:service:RenderingControl:1"
	ssdpAddr    = "239.255.255.250:1900"
)

// searchDLNA asks the network for media renderers and returns where their descriptions are.
func searchDLNA(ctx context.Context, wait time.Duration) ([]string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	to, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, err
	}
	secs := int(wait / time.Second)
	if secs < 1 {
		secs = 1
	}
	for _, st := range []string{"urn:schemas-upnp-org:device:MediaRenderer:1", avTransport} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: " + ssdpAddr + "\r\nMAN: \"ssdp:discover\"\r\nMX: " + strconv.Itoa(secs) + "\r\nST: " + st + "\r\n\r\n"
		// UDP is lossy: say it twice.
		for range 2 {
			if _, err := conn.WriteTo([]byte(msg), to); err != nil {
				return nil, err
			}
		}
	}
	deadline := time.Now().Add(wait + 500*time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetReadDeadline(deadline)
	seen := map[string]bool{}
	var out []string
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break // the deadline
		}
		if loc := ssdpLocation(buf[:n]); loc != "" && !seen[loc] {
			seen[loc] = true
			out = append(out, loc)
		}
	}
	return out, nil
}

// ssdpLocation reads the LOCATION of an SSDP answer, if it is an http address.
func ssdpLocation(b []byte) string {
	r, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(b)), nil)
	if err != nil {
		return ""
	}
	r.Body.Close()
	loc := r.Header.Get("Location")
	if u, err := url.Parse(loc); err != nil || u.Scheme != "http" || u.Host == "" {
		return ""
	}
	return loc
}

type upnpDevice struct {
	Type     string        `xml:"deviceType"`
	Name     string        `xml:"friendlyName"`
	Model    string        `xml:"modelName"`
	UDN      string        `xml:"UDN"`
	Services []upnpService `xml:"serviceList>service"`
	Devices  []upnpDevice  `xml:"deviceList>device"`
}

type upnpService struct {
	Type    string `xml:"serviceType"`
	Control string `xml:"controlURL"`
}

// dlnaDescription is what a renderer says about itself.
type dlnaDescription struct {
	Device              Device
	transport, volumeAt string
}

// describeDLNA reads a device description; ok is false when it cannot be told what to play.
func describeDLNA(ctx context.Context, client *http.Client, location string) (dlnaDescription, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return dlnaDescription{}, false, err
	}
	res, err := client.Do(req)
	if err != nil {
		return dlnaDescription{}, false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return dlnaDescription{}, false, fmt.Errorf("description: HTTP %d", res.StatusCode)
	}
	var root struct {
		Base   string     `xml:"URLBase"`
		Device upnpDevice `xml:"device"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&root); err != nil {
		return dlnaDescription{}, false, err
	}
	base, err := url.Parse(location)
	if err != nil {
		return dlnaDescription{}, false, err
	}
	if root.Base != "" {
		if b, err := url.Parse(root.Base); err == nil && b.Scheme == "http" {
			base = b
		}
	}
	// The renderer may be a device inside the root one.
	var find func(d upnpDevice) (dlnaDescription, bool)
	find = func(d upnpDevice) (dlnaDescription, bool) {
		var out dlnaDescription
		for _, s := range d.Services {
			ref, err := url.Parse(strings.TrimSpace(s.Control))
			if err != nil {
				continue
			}
			at := base.ResolveReference(ref).String()
			switch {
			case strings.HasPrefix(s.Type, "urn:schemas-upnp-org:service:AVTransport:"):
				out.transport = at
			case strings.HasPrefix(s.Type, "urn:schemas-upnp-org:service:RenderingControl:"):
				out.volumeAt = at
			}
		}
		if out.transport != "" {
			name := strings.TrimSpace(d.Name)
			if name == "" {
				name = strings.TrimSpace(d.Model)
			}
			out.Device = Device{ID: safeID("dlna-", strings.TrimPrefix(strings.TrimSpace(d.UDN), "uuid:")), Name: name, Kind: DLNA, Model: strings.TrimSpace(d.Model), Location: location}
			return out, out.Device.ID != "dlna"
		}
		for _, c := range d.Devices {
			if o, ok := find(c); ok {
				return o, true
			}
		}
		return out, false
	}
	d, ok := find(root.Device)
	return d, ok, nil
}

// dlnaRenderer controls one DLNA renderer by SOAP.
type dlnaRenderer struct {
	client   *http.Client
	location string

	mu    sync.Mutex
	desc  dlnaDescription
	ready bool
}

func newDLNA(client *http.Client, location string) *dlnaRenderer {
	return &dlnaRenderer{client: client, location: location}
}

// controls reads the description again when needed: a device that restarted may have moved.
func (r *dlnaRenderer) controls(ctx context.Context) (dlnaDescription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ready {
		return r.desc, nil
	}
	d, ok, err := describeDLNA(ctx, r.client, r.location)
	if err != nil {
		return d, err
	}
	if !ok {
		return d, ErrUnsupported
	}
	r.desc, r.ready = d, true
	return d, nil
}

func (r *dlnaRenderer) forget() { r.mu.Lock(); r.ready = false; r.mu.Unlock() }

// soapFault is a device's refusal.
type soapFault struct{ Code, Text string }

func (f soapFault) Error() string {
	if f.Text != "" {
		return "设备拒绝了：" + f.Text + "（" + f.Code + "）"
	}
	return "设备拒绝了（" + f.Code + "）"
}

// call runs one action and returns the fields of its answer.
func (r *dlnaRenderer) call(ctx context.Context, volume bool, action string, args [][2]string) (map[string]string, error) {
	d, err := r.controls(ctx)
	if err != nil {
		return nil, err
	}
	at, service := d.transport, avTransport
	if volume {
		at, service = d.volumeAt, rendering
		if at == "" {
			return nil, ErrUnsupported
		}
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	body.WriteString(`<u:` + action + ` xmlns:u="` + service + `">`)
	for _, a := range args {
		body.WriteString("<" + a[0] + ">" + html.EscapeString(a[1]) + "</" + a[0] + ">")
	}
	body.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, at, strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+service+"#"+action+`"`)
	res, err := r.client.Do(req)
	if err != nil {
		r.forget()
		return nil, err
	}
	defer res.Body.Close()
	fields, fault, err := readSOAP(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		if fault != nil {
			return nil, *fault
		}
		if res.StatusCode == http.StatusNotFound {
			r.forget()
		}
		return nil, fmt.Errorf("%s: HTTP %d", action, res.StatusCode)
	}
	return fields, err
}

// readSOAP collects the leaf elements of an answer by their local names.
func readSOAP(r io.Reader) (map[string]string, *soapFault, error) {
	dec := xml.NewDecoder(r)
	out := map[string]string{}
	var name string
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name = t.Name.Local
			text.Reset()
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if name == t.Name.Local {
				out[name] = strings.TrimSpace(text.String())
			}
			name = ""
		}
	}
	if code, ok := out["errorCode"]; ok {
		return out, &soapFault{Code: code, Text: out["errorDescription"]}, nil
	}
	return out, nil, nil
}

func clock(d time.Duration) string {
	s := int64(d / time.Second)
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}

func parseClock(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "NOT_IMPLEMENTED") {
		return 0
	}
	whole, frac, _ := strings.Cut(s, ".")
	parts := strings.Split(whole, ":")
	var total float64
	for _, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	if f, err := strconv.ParseFloat("0."+frac, 64); err == nil && frac != "" {
		total += f
	}
	return time.Duration(total * float64(time.Second))
}

// didl says what the address is, as most renderers want to be told.
func didl(m Media) string {
	kind := "object.item.audioItem.musicTrack"
	protocol := "http-get:*:" + m.Type + ":*"
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
	b.WriteString(`<item id="0" parentID="-1" restricted="1">`)
	b.WriteString("<dc:title>" + html.EscapeString(m.Title) + "</dc:title>")
	if m.Artist != "" {
		b.WriteString("<dc:creator>" + html.EscapeString(m.Artist) + "</dc:creator><upnp:artist>" + html.EscapeString(m.Artist) + "</upnp:artist>")
	}
	if m.Album != "" {
		b.WriteString("<upnp:album>" + html.EscapeString(m.Album) + "</upnp:album>")
	}
	b.WriteString("<upnp:class>" + kind + "</upnp:class>")
	b.WriteString(`<res protocolInfo="` + html.EscapeString(protocol) + `"`)
	if m.Duration > 0 {
		b.WriteString(` duration="` + clock(m.Duration) + `.000"`)
	}
	b.WriteString(">" + html.EscapeString(m.URL) + "</res></item></DIDL-Lite>")
	return b.String()
}

var instance = [2]string{"InstanceID", "0"}

func (r *dlnaRenderer) Load(ctx context.Context, m Media, start time.Duration) error {
	// Some renderers refuse a new address while one plays.
	_, _ = r.call(ctx, false, "Stop", [][2]string{instance})
	if _, err := r.call(ctx, false, "SetAVTransportURI", [][2]string{instance, {"CurrentURI", m.URL}, {"CurrentURIMetaData", didl(m)}}); err != nil {
		return err
	}
	if err := r.Play(ctx); err != nil {
		return err
	}
	if start < time.Second {
		return nil
	}
	// Most renderers seek only once they play.
	for i := 0; i < 40; i++ {
		st, err := r.Status(ctx)
		if err == nil && st.State == Playing {
			return r.Seek(ctx, start)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return r.Seek(ctx, start)
}

func (r *dlnaRenderer) Play(ctx context.Context) error {
	_, err := r.call(ctx, false, "Play", [][2]string{instance, {"Speed", "1"}})
	return err
}

func (r *dlnaRenderer) Pause(ctx context.Context) error {
	_, err := r.call(ctx, false, "Pause", [][2]string{instance})
	return err
}

func (r *dlnaRenderer) Stop(ctx context.Context) error {
	_, err := r.call(ctx, false, "Stop", [][2]string{instance})
	return err
}

func (r *dlnaRenderer) Seek(ctx context.Context, to time.Duration) error {
	_, err := r.call(ctx, false, "Seek", [][2]string{instance, {"Unit", "REL_TIME"}, {"Target", clock(to)}})
	return err
}

func (r *dlnaRenderer) SetVolume(ctx context.Context, v int) error {
	_, err := r.call(ctx, true, "SetVolume", [][2]string{instance, {"Channel", "Master"}, {"DesiredVolume", strconv.Itoa(v)}})
	return err
}

func (r *dlnaRenderer) Status(ctx context.Context) (Status, error) {
	t, err := r.call(ctx, false, "GetTransportInfo", [][2]string{instance})
	if err != nil {
		return Status{}, err
	}
	st := Status{Volume: -1}
	switch strings.ToUpper(t["CurrentTransportState"]) {
	case "PLAYING":
		st.State = Playing
	case "PAUSED_PLAYBACK", "PAUSED_RECORDING":
		st.State = Paused
	case "TRANSITIONING":
		st.State = Buffering
	default: // STOPPED, NO_MEDIA_PRESENT
		st.State = Stopped
	}
	if s := strings.ToUpper(t["CurrentTransportStatus"]); s != "" && s != "OK" {
		st.State = Failed
	}
	if p, err := r.call(ctx, false, "GetPositionInfo", [][2]string{instance}); err == nil {
		st.Position, st.Duration, st.URL = parseClock(p["RelTime"]), parseClock(p["TrackDuration"]), p["TrackURI"]
	}
	if v, err := r.call(ctx, true, "GetVolume", [][2]string{instance, {"Channel", "Master"}}); err == nil {
		if n, err := strconv.Atoi(v["CurrentVolume"]); err == nil && n >= 0 && n <= 100 {
			st.Volume = n
		}
	}
	return st, nil
}

func (r *dlnaRenderer) Close() {}
