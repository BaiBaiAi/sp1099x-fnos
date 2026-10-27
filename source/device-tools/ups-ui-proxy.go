package main

import("encoding/json";"log";"net/http";"net/http/httputil";"net/url";"os";"path/filepath";"strconv";"strings";"time")

func telemetry(w http.ResponseWriter,r *http.Request){
 b:="/sys/class/power_supply/battery/"; data:=map[string]interface{}{"timestamp":time.Now().Unix()}
 for _,name:=range []string{"status","capacity","voltage_now","current_now","temp","present"}{
  raw,err:=os.ReadFile(b+name);if err!=nil{http.Error(w,"Battery measurement unavailable",503);return}
  value:=strings.TrimSpace(string(raw));if name=="status"{data[name]=value}else{n,err:=strconv.ParseInt(value,10,64);if err!=nil{http.Error(w,"Invalid battery measurement",503);return};data[name]=n}
 }
 soc:=data["capacity"].(int64);data["capacity_valid"]=soc>=0&&soc<=100
 if raw,err:=os.ReadFile("/var/lib/sp1099x/battery-runtime.json");err==nil{
  var estimate map[string]interface{}
  if json.Unmarshal(raw,&estimate)==nil{if stamp,ok:=estimate["timestamp"].(float64);ok&&time.Now().Unix()-int64(stamp)>=0&&time.Now().Unix()-int64(stamp)<45{data["runtime"]=estimate}}
 }
 w.Header().Set("Content-Type","application/json");w.Header().Set("Cache-Control","no-store")
 json.NewEncoder(w).Encode(data)
}
func main(){
 upstream,_:=url.Parse("http://127.0.0.1:5666")
 proxy:=httputil.NewSingleHostReverseProxy(upstream)
 files:=map[string]string{"/assets/index-BERCugxW.js":"index-BERCugxW.js","/assets/Setting-B-IcJ4nT.js":"Setting-B-IcJ4nT.js"}
 mux:=http.NewServeMux()
 mux.HandleFunc("/",func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/sp1099x-ups/status.json"{telemetry(w,r);return}
  if r.URL.Path=="/sp1099x-ups/health"{w.Header().Set("Cache-Control","no-store");w.Write([]byte("ok"));return}
  if name,ok:=files[r.URL.Path];ok{
   w.Header().Set("Cache-Control","no-store");w.Header().Set("Content-Type","application/javascript; charset=utf-8")
   http.ServeFile(w,r,filepath.Join("/usr/local/libexec/sp1099x/ups-ui",name));return
  }
  proxy.ServeHTTP(w,r)
 })
 log.Print("Native UPS UI compatibility gateway on :5668; vendor server stays on loopback :5666")
 server:=http.Server{Addr:":5668",Handler:mux,ReadHeaderTimeout:15*time.Second,IdleTimeout:90*time.Second}
 log.Fatal(server.ListenAndServe())
}
