package casc

type LocaleFlag uint32

const (
	LocaleEnUS LocaleFlag = 1 << iota
	LocaleKoKR
	LocaleFrFR
	LocaleDeDE
	LocaleZhCN
	LocaleEsES
	LocaleZhTW
	LocaleEnGB
	LocaleEsMX
	LocaleRuRU
	LocalePtBR
	LocaleItIT
	LocalePtPT
)

var localeNames = map[LocaleFlag]string{
	LocaleEnUS: "enUS", LocaleKoKR: "koKR", LocaleFrFR: "frFR",
	LocaleDeDE: "deDE", LocaleZhCN: "zhCN", LocaleEsES: "esES",
	LocaleZhTW: "zhTW", LocaleEnGB: "enGB", LocaleEsMX: "esMX",
	LocaleRuRU: "ruRU", LocalePtBR: "ptBR", LocaleItIT: "itIT",
	LocalePtPT: "ptPT",
}

func LocaleFlagByName(name string) LocaleFlag {
	for f, n := range localeNames {
		if n == name {
			return f
		}
	}
	return LocaleEnUS
}

func (f LocaleFlag) Name() string {
	if n, ok := localeNames[f]; ok {
		return n
	}
	return "enUS"
}
