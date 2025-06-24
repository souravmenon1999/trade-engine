package injective

import (
	"context"
	"fmt"
	"strconv"
	"time"

	derivativeExchangePB "github.com/InjectiveLabs/sdk-go/exchange/derivative_exchange_rpc/pb"
	logs "github.com/rs/zerolog/log"
	"github.com/souravmenon1999/trade-engine/framedCopy/types"
)

func (c *InjectiveClient) parseFundingRate(res *derivativeExchangePB.StreamMarketResponse) (*types.FundingRate, error) {
	if res.Market == nil || res.Market.PerpetualMarketInfo == nil || res.Market.PerpetualMarketFunding == nil {
		return nil, fmt.Errorf("missing market, perpetual market info, or funding data in response")
	}
	rate, interval := c.GetFundingRateAndInterval()
	if res.Market.PerpetualMarketInfo.FundingInterval > 0 {
		interval = time.Duration(res.Market.PerpetualMarketInfo.FundingInterval) * time.Second
		c.UpdateFundingRateAndInterval(rate, interval) // Update interval dynamically
	}
	return &types.FundingRate{
		Rate:                 rate,
		Interval:             interval,
		LastUpdated:          res.Market.PerpetualMarketFunding.LastTimestamp,
		NextFundingTimestamp: res.Market.PerpetualMarketInfo.NextFundingTimestamp,
	}, nil
}

func (c *InjectiveClient) UpdateFundingRateAndInterval(rate float64, interval time.Duration) {
	c.fundingRateMu.Lock()
	defer c.fundingRateMu.Unlock()
	c.fundingRate = rate
	c.fundingInterval = interval
	logs.Info().
		Float64("funding_rate", rate).
		Str("funding_interval", interval.String()).
		Msg("Updated actual funding rate and interval")
}

func (c *InjectiveClient) GetFundingRateAndInterval() (float64, time.Duration) {
	c.fundingRateMu.Lock()
	defer c.fundingRateMu.Unlock()
	return c.fundingRate, c.fundingInterval
}

func (c *InjectiveClient) FetchScheduledFundingRates(marketId string, ctx context.Context, nextFundingTimestamp int64, fundingInterval int64) {

	// Initial fetch
	req := &derivativeExchangePB.FundingRatesRequest{MarketId: marketId}
	res, err := c.updatesClient.GetDerivativeFundingRates(ctx, req)
	if err != nil {
		logs.Error().Err(err).Msg("Failed to fetch initial funding rates")
	} else if len(res.FundingRates) > 0 {
		rate, err := strconv.ParseFloat(res.FundingRates[0].Rate, 64)
		if err != nil {
			logs.Error().Err(err).Msg("Invalid initial funding rate")
		} else {
			c.UpdateFundingRateAndInterval(rate, time.Duration(fundingInterval)*time.Second)
		}
	}

	// Schedule fetches based on next_funding_timestamp
	go func() {
		currentNextFundingTimestamp := nextFundingTimestamp
		currentFundingInterval := fundingInterval
		for {
			select {
			case <-ctx.Done():
				logs.Info().Msg("Stopped scheduled funding rate fetch")
				return
			default:
				// Calculate time until next funding event (plus 2-second buffer)
				currentTime := time.Now().Unix()
				durationUntilNext := time.Duration(currentNextFundingTimestamp-currentTime+2) * time.Second
				if durationUntilNext < 0 {
					// Missed the event, schedule for next interval
					currentNextFundingTimestamp += currentFundingInterval
					durationUntilNext = time.Duration(currentNextFundingTimestamp-currentTime+2) * time.Second
				}

				// Wait until the next funding event
				timer := time.NewTimer(durationUntilNext)
				select {
				case <-ctx.Done():
					timer.Stop()
					logs.Info().Msg("Stopped scheduled funding rate fetch")
					return
				case <-timer.C:
					// Fetch the actual funding rate
					res, err := c.updatesClient.GetDerivativeFundingRates(ctx, req)
					if err != nil {
						logs.Error().Err(err).Msg("Failed to fetch funding rates")
					} else if len(res.FundingRates) > 0 {
						rate, err := strconv.ParseFloat(res.FundingRates[0].Rate, 64)
						if err != nil {
							logs.Error().Err(err).Msg("Invalid funding rate")
						} else {
							// Update with current interval
							_, interval := c.GetFundingRateAndInterval()
							c.UpdateFundingRateAndInterval(rate, interval)
						}
					}
					// Schedule next fetch
					currentNextFundingTimestamp += currentFundingInterval
				}
			}
		}
	}()
}
