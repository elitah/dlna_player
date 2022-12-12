package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"strings"

	"embed"

	"github.com/elitah/chanpool"
)

//go:embed xml/*
var fs embed.FS

type Buffer struct {
	Data   [4 * 1024]byte
	Length int

	address *net.UDPAddr

	offset int
}

func (this *Buffer) Reset() {
	//
	this.offset = 0
}

func (this *Buffer) FixupTailWithNewLine(n int) bool {
	//
	if 4 < n {
		//
		tailHex := (uint32(this.Data[n-4]) << 24) |
			(uint32(this.Data[n-3]) << 16) |
			(uint32(this.Data[n-2]) << 8) |
			(uint32(this.Data[n-1]) << 0)
		//
		if 0xD0A0D0A != tailHex {
			//
			if 0xD0A == tailHex&0xFFFF {
				//
				n += copy(this.Data[n:], []byte{0xD, 0xA})
			} else {
				//
				n += copy(this.Data[n:], []byte{0xD, 0xA, 0xD, 0xA})
			}
		}
		//
		//fmt.Printf("%X\n", this.Data[:n])
		//
		this.Length = n
		//
		return true
	}
	//
	return false
}

func (this *Buffer) Read(p []byte) (int, error) {
	//
	if this.Length > this.offset {
		//
		var n = copy(p, this.Data[this.offset:this.Length])
		//
		this.offset += n
		//
		return n, nil
	} else {
		//
		return 0, io.EOF
	}
}

func (this *Buffer) String() string {
	//
	return string(this.Data[:this.Length])
}

type myChanPool struct {
	//
	chanpool.ChanPool
}

func (this *myChanPool) Get() *Buffer {
	//
	if v, ok := this.ChanPool.Get().(*Buffer); ok {
		//
		return v
	}
	//
	return nil
}

func (this *myChanPool) Put(p *Buffer) {
	//
	p.Reset()
	//
	p.Length = 0
	//
	this.ChanPool.Put(p)
}

func checkInterface(iface *net.Interface) bool {
	//
	if net.FlagUp != iface.Flags&net.FlagUp {
		//
		return false
	}
	//
	if net.FlagBroadcast != iface.Flags&net.FlagBroadcast {
		//
		return false
	}
	//
	if net.FlagLoopback == iface.Flags&net.FlagLoopback {
		//
		return false
	}
	//
	if net.FlagPointToPoint == iface.Flags&net.FlagPointToPoint {
		//
		return false
	}
	//
	if net.FlagMulticast != iface.Flags&net.FlagMulticast {
		//
		return false
	}
	//
	if list, err := iface.Addrs(); nil != err {
		//
		return false
	} else if 0 == len(list) {
		//
		return false
	} else {
		//
		found := false
		//
		for _, item := range list {
			//
			if ip, _, err := net.ParseCIDR(item.String()); nil == err {
				//
				found = nil != ip.To4()
				//
				if found {
					//
					break
				}
			}
		}
		//
		if !found {
			//
			return false
		}
	}
	//
	return true
}

func multicastAtInterface(pool *myChanPool, ch chan *Buffer, name string, args ...string) error {
	//
	if iface, err := net.InterfaceByName(name); nil == err {
		//
		var designee string
		//
		for _, item := range args {
			//
			if "" != item {
				//
				designee = item
			}
		}
		//
		fmt.Println(name, "start listening...")
		//
		if conn, err := net.ListenMulticastUDP("udp4", iface, &net.UDPAddr{
			IP:   net.IPv4(239, 255, 255, 250),
			Port: 1900,
		}); nil == err {
			//
			go func(conn *net.UDPConn) {
				//
				var buf *Buffer
				//
				defer conn.Close()
				//
				for {
					//
					if nil == buf {
						//
						buf = pool.Get()
					}
					//
					if nil == buf {
						//
						return
					}
					//
					if n, address, err := conn.ReadFromUDP(buf.Data[:]); nil == err {
						//
						if "" != designee && designee != address.IP.String() {
							//
							continue
						}
						//
						if buf.FixupTailWithNewLine(n) {
							//
							buf.address = address
							//
							ch <- buf
							//
							buf = nil
						}
					}
				}
			}(conn)
			//
			return nil
		} else {
			//
			return err
		}
	} else {
		//
		return err
	}
}

func processDeviceSearch(pool *myChanPool, ch chan *Buffer, uuid string, httpport int, designee string) {
	//
	if list, err := net.Interfaces(); nil == err {
		//
		for _, item := range list {
			//
			if checkInterface(&item) {
				//
				if err := multicastAtInterface(pool, ch, item.Name, designee); nil != err {
					//
					fmt.Println("multicastAtInterface:", err)
				}
			} else {
				//
				//fmt.Println(item.Name, "xxxxxxxxxxxxxxxxxxxx>")
			}
		}
	}
	//
	for {
		//
		select {
		case buf, ok := <-ch:
			//
			if ok {
				//
				tp := textproto.NewReader(bufio.NewReader(buf))
				//
				if s, err := tp.ReadLine(); nil == err {
					//
					if strings.HasPrefix(s, "M-SEARCH * ") {
						//
						if h, err := tp.ReadMIMEHeader(); nil == err {
							//
							if stField := h.Get("ST"); strings.Contains(stField, "upnp:rootdevice") || true {
								//
								if manField := h.Get("MAN"); strings.Contains(manField, "ssdp:discover") {
									//
									if conn, err := net.DialUDP("udp4", nil, buf.address); nil == err {
										//
										if address, ok := conn.LocalAddr().(*net.UDPAddr); ok {
											//
											var b bytes.Buffer
											//
											b.WriteString("HTTP/1.1 200 OK\r\n")
											//
											fmt.Fprintf(
												&b,
												"ST: %s\r\nUSN: uuid:%s::upnp:rootdevice\r\nLocation: http://%s:%d/description.xml\r\n",
												stField,
												uuid,
												address.IP.String(),
												httpport,
											)
											//
											b.WriteString("OPT: \"http://schemas.upnp.org/upnp/1/0/\"; ns=01\r\n")
											b.WriteString("Cache-Control: max-age=300\r\n")
											b.WriteString("Server: DLNA-Proxy-Server\r\n")
											b.WriteString("Ext: \r\n")
											b.WriteString("\r\n")
											//
											conn.Write(b.Bytes())
										}
										//
										conn.Close()
									} else {
										//
										fmt.Println("net.DialUDP:", err)
									}
								} else {
									//
									//fmt.Println("no ssdp:discover")
								}
							} else {
								//
								//fmt.Println("no upnp:rootdevice")
							}
						} else {
							//
							fmt.Println("tp.ReadMIMEHeader:", err)
						}
					} else {
						//
						//fmt.Println("not M-SEARCH message")
					}
				} else {
					//
					fmt.Println("tp.ReadLine:", err)
				}
				//
				pool.Put(buf)
			} else {
				//
				return
			}
		}
	}
}

func uuid_get() string {
	//
	b := make([]byte, 16)
	//
	_, err := rand.Read(b)
	//
	if nil != err {
		//
		return "00000000-0000-0000-0000-000000000000"
	}
	//
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func main() {
	//
	var port int
	//
	var name string
	//
	var uuid string
	//
	var shell string
	//
	var ffplay string
	//
	flag.StringVar(&name, "n", "", "your device name")
	flag.StringVar(&uuid, "u", "", "your uuid string")
	flag.StringVar(&shell, "s", "", "your shell script file path")
	flag.StringVar(&ffplay, "f", "", "your ffplay path")
	//
	flag.Parse()
	//
	if "" == uuid {
		//
		name = os.Args[0]
	}
	//
	if "" == uuid {
		//
		uuid = uuid_get()
	}
	//
	if l, err := net.Listen("tcp4", ":0"); nil == err {
		//
		if addr, ok := l.Addr().(*net.TCPAddr); ok {
			//
			port = addr.Port
		}
		//
		hfs := http.FileServer(http.FS(fs))
		//
		go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			//
			fmt.Println(r.Method, r.Host, r.URL.String())
			//
			if "GET" == r.Method {
				//
				if "/description.xml" == r.URL.Path {
					//
					host, _, _ := net.SplitHostPort(r.Host)
					//
					if "" == host {
						//
						host = r.Host
					}
					//
					w.Header().Set("Content-Type", "text/xml; charset=utf-8")
					//
					fmt.Fprintf(
						w,
						`<?xml version="1.0" encoding="utf-8"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
   <specVersion>
      <major>1</major>
      <minor>0</minor>
   </specVersion>
   <URLBase>http://%s:%d/</URLBase>
   <device>
      <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
      <friendlyName>%s</friendlyName>
      <manufacturer></manufacturer>
      <manufacturerURL></manufacturerURL>
      <modelDescription></modelDescription>
      <modelName></modelName>
      <modelNumber></modelNumber>
      <modelURL></modelURL>
      <UDN>uuid:%s</UDN>
      <dlna:X_DLNADOC xmlns:dlna="urn:schemas-dlna-org:device-1-0">DMR-1.50</dlna:X_DLNADOC>
      <serviceList>
         <service>
            <serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType>
            <serviceId>urn:upnp-org:serviceId:AVTransport</serviceId>
            <SCPDURL>AVTransport.scpd.xml</SCPDURL>
            <controlURL>_urn:schemas-upnp-org:service:AVTransport_control</controlURL>
            <eventSubURL>_urn:schemas-upnp-org:service:AVTransport_event</eventSubURL>
         </service>
      </serviceList>
   </device>
</root>
`,
						host,
						port,
						name,
						uuid,
					)
					//
					return
				}
				//
				r.URL.Path = "xml" + r.URL.Path
				//
				hfs.ServeHTTP(w, r)
			} else {
				//
				var b bytes.Buffer
				//
				io.Copy(&b, r.Body)
				//
				if strings.HasSuffix(
					r.Header.Get("SOAPAction"),
					"#SetAVTransportURI\"",
				) {
					//
					var result struct {
						EncodingStyle string `xml:"encodingStyle,attr"`
						Soap          string `xml:"s,attr"`
						Body          struct {
							SetAVTransportURI struct {
								URN                string `xml:"u,attr"`
								InstanceID         int    `xml:"InstanceID"`
								CurrentURI         string `xml:"CurrentURI"`
								CurrentURIMetaData string `xml:"CurrentURIMetaData"`
							}
						}
					}
					//
					//fmt.Println(b.String())
					//
					if err := xml.Unmarshal(b.Bytes(), &result); nil == err {
						//
						//fmt.Printf("%+v\n", result)
						//
						if "" != result.Body.SetAVTransportURI.CurrentURI {
							//
							fmt.Println(result.Body.SetAVTransportURI.CurrentURI)
							//
							if "" != shell {
								//
								if bash_path := os.Getenv("SHELL"); "" != bash_path {
									//
									if cwd, err := os.Readlink("/proc/self/cwd"); nil == err {
										//
										go func(path, dir, shell, url string) {
											//
											if p, err := os.StartProcess(
												path,
												[]string{
													"bash",
													shell,
													url,
												},
												&os.ProcAttr{
													Dir:   dir,
													Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
												},
											); nil == err {
												//
												p.Wait()
											}
										}(bash_path, cwd, shell, result.Body.SetAVTransportURI.CurrentURI)
									}
								}
							} else if "" != ffplay {
								//
								go func(path, url string) {
									//
									if p, err := os.StartProcess(
										path,
										[]string{
											"ffplay",
											"-showmode", "0",
											"-x", "100",
											"-y", "100",
											url,
										},
										&os.ProcAttr{
											Dir:   ".",
											Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
										},
									); nil == err {
										//
										p.Wait()
									} else {
										//
										fmt.Println(err)
									}
								}(ffplay, result.Body.SetAVTransportURI.CurrentURI)
							}
						}
					} else {
						//
						fmt.Println("xml.Unmarshal:", err)
					}
				}
			}
		}))
	} else {
		//
		fmt.Println(err)
	}
	//
	fmt.Println("port:", port)
	//
	pool := myChanPool{
		ChanPool: chanpool.NewChanPool(32, func() interface{} {
			return &Buffer{}
		}),
	}
	//
	ch := make(chan *Buffer, 32)
	//
	go processDeviceSearch(&pool, ch, uuid, port, "")
	//
	select {}
}
