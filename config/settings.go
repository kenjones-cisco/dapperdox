package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/spf13/viper"
)

var errInvalidDiscoveryDuration = errors.New("discovery periods and timeout must be positive; resync interval and initial delay must be nonnegative")

// Settings is a detached configuration snapshot. Owners clone collections at construction.
type Settings struct {
	LogLevel, BindAddr, ManagementAddr, TLSCert, TLSKey, SiteURL                              string
	DefaultAssetsDir, AssetsDir, Theme, ThemeDir                                              string
	SpecDir, SpecDefaultHost, SpecRewrites                                                    string
	DiscoveryDomain, DiscoveryNamespace, DiscoverySuffix, DiscoveryGroupingKey                string
	ProxyPath, DocumentRewriteURL, SpecRewriteURL, SpecGroupings, DiscoveryGroupingConverters map[string]string
	AllowOrigin, SpecFilename, DiscoveryServiceIgnoreList                                     []string
	DiscoveryInterval, DiscoveryInitialDelay, DiscoverySpecLoadTimeout, DiscoveryPeriodTime   time.Duration
	ShowAssets, ForceSpecList, DiscoveryEnabled                                               bool
}

// Clone isolates collection settings from their caller.
func (s Settings) Clone() Settings {
	s.ProxyPath = maps.Clone(s.ProxyPath)
	s.DocumentRewriteURL = maps.Clone(s.DocumentRewriteURL)
	s.SpecRewriteURL = maps.Clone(s.SpecRewriteURL)
	s.SpecGroupings = maps.Clone(s.SpecGroupings)
	s.DiscoveryGroupingConverters = maps.Clone(s.DiscoveryGroupingConverters)
	s.AllowOrigin = slices.Clone(s.AllowOrigin)
	s.SpecFilename = slices.Clone(s.SpecFilename)
	s.DiscoveryServiceIgnoreList = slices.Clone(s.DiscoveryServiceIgnoreList)

	return s
}

// Snapshot adapts legacy Viper callers at the construction boundary.
func Snapshot() Settings {
	return Settings{
		LogLevel: viper.GetString(LogLevel), BindAddr: viper.GetString(BindAddr), ManagementAddr: viper.GetString(ManagementAddr),
		TLSCert: viper.GetString(TLSCert), TLSKey: viper.GetString(TLSKey), SiteURL: viper.GetString(SiteURL),
		DefaultAssetsDir: viper.GetString(DefaultAssetsDir), AssetsDir: viper.GetString(AssetsDir), Theme: viper.GetString(Theme), ThemeDir: viper.GetString(ThemeDir),
		SpecDir: viper.GetString(SpecDir), SpecDefaultHost: viper.GetString(SpecDefaultHost), SpecRewrites: viper.GetString(SpecRewrites),
		DiscoveryDomain: viper.GetString(DiscoveryDomain), DiscoveryNamespace: viper.GetString(DiscoveryNamespace),
		DiscoverySuffix: viper.GetString(DiscoverySuffix), DiscoveryGroupingKey: viper.GetString(DiscoveryGroupingKey),
		ProxyPath: viper.GetStringMapString(ProxyPath), DocumentRewriteURL: viper.GetStringMapString(DocumentRewriteURL),
		SpecRewriteURL: viper.GetStringMapString(SpecRewriteURL), SpecGroupings: viper.GetStringMapString(SpecGroupings),
		DiscoveryGroupingConverters: viper.GetStringMapString(DiscoveryGroupingConverters),
		AllowOrigin:                 viper.GetStringSlice(AllowOrigin), SpecFilename: viper.GetStringSlice(SpecFilename), DiscoveryServiceIgnoreList: viper.GetStringSlice(DiscoveryServiceIgnoreList),
		DiscoveryInterval: viper.GetDuration(DiscoveryInterval), DiscoveryInitialDelay: viper.GetDuration(DiscoveryInitialDelay),
		DiscoverySpecLoadTimeout: viper.GetDuration(DiscoverySpecLoadTimeout), DiscoveryPeriodTime: viper.GetDuration(DiscoveryPeriodTime),
		ShowAssets: viper.GetBool(ShowAssets), ForceSpecList: viper.GetBool(ForceSpecList), DiscoveryEnabled: viper.GetBool(DiscoveryEnabled),
	}.Clone()
}

// Load reads optional configuration, preserving flag > environment > file > default precedence.
func Load() (Settings, error) {
	viper.SetConfigName("config")

	if dir := viper.GetString(cfgDirKey); dir != "" {
		viper.AddConfigPath(dir)
	}

	for _, path := range defaultConfigPaths {
		viper.AddConfigPath(path)
	}

	if err := viper.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if !errors.As(err, &missing) {
			return Settings{}, fmt.Errorf("read configuration: %w", err)
		}
	}

	for _, key := range []string{DiscoveryInterval, DiscoveryInitialDelay, DiscoverySpecLoadTimeout, DiscoveryPeriodTime} {
		var duration time.Duration
		if err := viper.UnmarshalKey(key, &duration); err != nil {
			return Settings{}, fmt.Errorf("configuration %s: %w", key, err)
		}
	}

	for _, key := range []string{ShowAssets, ForceSpecList, DiscoveryEnabled} {
		var enabled bool
		if err := viper.UnmarshalKey(key, &enabled); err != nil {
			return Settings{}, fmt.Errorf("configuration %s: %w", key, err)
		}
	}

	settings := Snapshot()
	if settings.DiscoveryEnabled && (settings.DiscoveryPeriodTime <= 0 || settings.DiscoverySpecLoadTimeout <= 0 || settings.DiscoveryInterval < 0 || settings.DiscoveryInitialDelay < 0) {
		return Settings{}, errInvalidDiscoveryDuration
	}

	return settings, nil
}
