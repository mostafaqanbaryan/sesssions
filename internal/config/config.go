package config

type Config struct {
	Projects []string
}

func NewConfig() *Config {
	return &Config{
		Projects: []string{
			"/home/mostafa/Projects/",
			"/home/mostafa/dotfiles",
		},
	}
}
