package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"messeances/api/internal/accountmail"
	"messeances/api/internal/accounts"
	runtimeconfig "messeances/api/internal/config"
)

// Provider initialization failures affect only mail, not public/admin readiness.
// SDK credential retrieval is lazy and all worker requests honor shutdown.
func runAccountMail(ctx context.Context, pool *pgxpool.Pool, cfg runtimeconfig.AccountsConfig, logger *slog.Logger) {
	if !cfg.Enabled {
		return
	}
	for ctx.Err() == nil {
		initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ses, queue, err := accountmail.NewAWS(initCtx, cfg.AWSRegion)
		cancel()
		if err == nil {
			cipher, cipherErr := accountmail.NewCipher(cfg.OutboxKeyID, cfg.OutboxKey[:], nil)
			if cipherErr != nil {
				logger.Warn("account_mail_unavailable")
				return
			}
			address := func(raw string) ([]byte, error) {
				email, err := accounts.NormalizeEmail(raw)
				if err != nil {
					return nil, accountmail.ErrRejected
				}
				digest, err := accounts.AddressKey(cfg.AddressHMACKey[:], "suppression", email)
				if err != nil {
					return nil, accountmail.ErrRejected
				}
				return digest[:], nil
			}
			worker := &accountmail.Worker{Pool: pool, Cipher: cipher, Address: address, Sender: &accountmail.SESSender{Client: ses, From: cfg.SESFromEmail, IdentityARN: cfg.SESIdentityARN, ConfigurationSet: cfg.SESConfigurationSet}}
			feedback := &accountmail.Feedback{Client: queue, Pool: pool, Address: address, QueueURL: cfg.SESFeedbackQueueURL, TopicARN: cfg.SESFeedbackTopicARN, IdentityARN: cfg.SESIdentityARN, From: cfg.SESFromEmail, ConfigurationSet: cfg.SESConfigurationSet}
			var workers sync.WaitGroup
			workers.Add(2)
			go func() { defer workers.Done(); worker.Run(ctx, logger) }()
			go func() { defer workers.Done(); feedback.Run(ctx, logger) }()
			workers.Wait()
			return
		}
		logger.Warn("account_mail_unavailable")
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
