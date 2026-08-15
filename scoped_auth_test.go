package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyCredentialsMigrateToHashesAndLeaveDisk(t *testing.T) {
	dir:=t.TempDir(); path:=filepath.Join(dir,"config.json")
	cfg:=Config{PublicAPIKeys:[]string{"legacy-public"},AdminToken:"legacy-admin",Providers:[]Provider{{ID:"p",Name:"P",Type:"openai",BaseURL:"http://example.test/v1",Enabled:true}}}
	normalized,err:=normalizeConfig(cfg);if err!=nil{t.Fatal(err)}
	if normalized.AdminToken!=""||len(normalized.PublicAPIKeys)!=0{t.Fatalf("legacy credentials remained in normalized config")}
	if len(normalized.APIKeys)!=2{t.Fatalf("api keys=%d want 2",len(normalized.APIKeys))}
	if err:=writeConfigAtomic(path,normalized);err!=nil{t.Fatal(err)}
	data,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
	text:=string(data);if strings.Contains(text,"legacy-public")||strings.Contains(text,"legacy-admin"){t.Fatalf("plaintext credential persisted: %s",text)}
	if !strings.Contains(text,hashAPIKey("legacy-public"))||!strings.Contains(text,hashAPIKey("legacy-admin")){t.Fatalf("hashes missing from persisted config")}
}

func TestPublicScopedKeyCannotUseAdminAPI(t *testing.T) {
	app:=testApp(t,Config{PublicAPIKeys:[]string{"client-key"},Providers:[]Provider{{ID:"p",Name:"P",Type:"openai",BaseURL:"http://example.test/v1",Enabled:true}}})
	req:=httptest.NewRequest(http.MethodGet,"/admin/api/providers",nil);req.Header.Set("Authorization","Bearer client-key");rec:=httptest.NewRecorder();app.routes().ServeHTTP(rec,req)
	if rec.Code!=http.StatusForbidden{t.Fatalf("status=%d body=%s",rec.Code,rec.Body.String())}
}

func TestScopedAPIKeyCreationStoresOnlyHash(t *testing.T) {
	app:=testApp(t,Config{Providers:[]Provider{{ID:"p",Name:"P",Type:"openai",BaseURL:"http://example.test/v1",Enabled:true}}})
	req:=httptest.NewRequest(http.MethodPost,"/admin/api/api-keys",strings.NewReader(`{"name":"Hermes","permissions":["chat","models.read"]}`));req.Header.Set("Content-Type","application/json");authorizeAdmin(req,app);rec:=httptest.NewRecorder();app.routes().ServeHTTP(rec,req)
	if rec.Code!=http.StatusCreated{t.Fatalf("status=%d body=%s",rec.Code,rec.Body.String())}
	body:=rec.Body.String();start:=strings.Index(body,"rtk_");if start<0{t.Fatalf("plaintext key not returned once: %s",body)}
	var found bool
	for _,k:=range app.store.Snapshot().APIKeys{if k.Name=="Hermes"{found=true;if k.Hash==""||strings.Contains(k.Hash,"rtk_"){t.Fatalf("key not hashed: %#v",k)};if !strings.Contains(body,k.Prefix){t.Fatalf("prefix absent from creation response")}}}
	if !found{t.Fatal("created key not stored")}
}

func TestRooterUpstreamTimeout(t *testing.T) {
	d,err:=rooterUpstreamTimeout("20m");if err!=nil{t.Fatal(err)};if d!=20*time.Minute{t.Fatalf("timeout=%s",d)}
	if _,err:=rooterUpstreamTimeout("nope");err==nil{t.Fatal("invalid timeout accepted")}
}

type flushRecorder struct{*httptest.ResponseRecorder;flushes int}
func (r *flushRecorder) Flush(){r.flushes++}

func TestSSEMiddlewareFlushesEachWrite(t *testing.T) {
	rec:=&flushRecorder{ResponseRecorder:httptest.NewRecorder()}
	h:=sseFlushMiddleware(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","text/event-stream");_,_=w.Write([]byte("data: one\n\n"));_,_=w.Write([]byte("data: two\n\n"))}))
	h.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/stream",nil))
	if rec.flushes!=2{t.Fatalf("flushes=%d want 2",rec.flushes)}
}

func TestAdminHTMLUsesKeyManager(t *testing.T) {
	if !strings.Contains(adminHTMLV2,"apiKeyCards")||!strings.Contains(adminHTMLV2,"Create key"){t.Fatal("scoped API key manager missing")}
	if !strings.Contains(adminHTMLV2,"id=\"publicKeys\" class=\"hidden\""){t.Fatal("legacy public key field is not hidden")}
}
