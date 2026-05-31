package casc

type LocaleFlag uint32

const (
	LocaleEnUS LocaleFlag = 0x2
	LocaleKoKR LocaleFlag = 0x4
	LocaleFrFR LocaleFlag = 0x10
	LocaleDeDE LocaleFlag = 0x20
	LocaleZhCN LocaleFlag = 0x40
	LocaleEsES LocaleFlag = 0x80
	LocaleZhTW LocaleFlag = 0x100
	LocaleEnGB LocaleFlag = 0x200
	LocaleEsMX LocaleFlag = 0x1000
	LocaleRuRU LocaleFlag = 0x2000
	LocalePtBR LocaleFlag = 0x4000
	LocaleItIT LocaleFlag = 0x8000
	LocalePtPT LocaleFlag = 0x10000
)

var localeNames = map[LocaleFlag]string{
	LocaleEnUS: "enUS", LocaleKoKR: "koKR", LocaleFrFR: "frFR",
	LocaleDeDE: "deDE", LocaleZhCN: "zhCN", LocaleEsES: "esES",
	LocaleZhTW: "zhTW", LocaleEnGB: "enGB", LocaleEsMX: "esMX",
	LocaleRuRU: "ruRU", LocalePtBR: "ptBR", LocaleItIT: "itIT",
	LocalePtPT: "ptPT",
}

func LocaleFlagByName(name string) LocaleFlag {
	if f, ok := LocaleFlagByNameOK(name); ok {
		return f
	}
	return LocaleZhCN
}

func LocaleFlagByNameOK(name string) (LocaleFlag, bool) {
	for f, n := range localeNames {
		if n == name {
			return f, true
		}
	}
	return 0, false
}

func (f LocaleFlag) Name() string {
	if n, ok := localeNames[f]; ok {
		return n
	}
	return "zhCN"
}
