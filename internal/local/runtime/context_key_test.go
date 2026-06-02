package runtime

import "testing"

func TestRemoteContextKeyIncludesBuildAndLocale(t *testing.T) {
	key := RemoteContextKey{
		Region: "cn", Product: "wow", BuildKey: "abcd", Locale: "zhCN", CacheRoot: `D:\cache`,
	}.String()
	want := "remote\x00cn\x00wow\x00abcd\x00zhCN\x00D:\\cache"
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
}

func TestLocalContextKeyIncludesCleanPathAndBuild(t *testing.T) {
	key := LocalContextKey{
		Path: `D:\World of Warcraft\_retail_\..\_retail_`, Product: "wow", BuildKey: "efgh", Locale: "zhCN",
	}.String()
	if key == "" {
		t.Fatal("local key is empty")
	}
	if key == (RemoteContextKey{Region: "cn", Product: "wow", BuildKey: "efgh", Locale: "zhCN"}).String() {
		t.Fatalf("local key must not equal remote key: %q", key)
	}
}
