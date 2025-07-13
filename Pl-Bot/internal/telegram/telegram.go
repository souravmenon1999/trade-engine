package telegram

import (
    "context"
    "fmt"
    "log"
    "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    //"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/pnl/injective"
	"github.com/souravmenon1999/trade-engine/Pl-Bot/internal/cache/injective"
)


type TelegramConfig struct {
    BotToken  string
    ChannelID string
}

func NewBot(config TelegramConfig) (*tgbotapi.BotAPI, error) {
    bot, err := tgbotapi.NewBotAPI(config.BotToken)
    if err != nil {
        return nil, fmt.Errorf("failed to create Telegram bot: %v", err)
    }
    log.Printf("Telegram bot initialized: %s", bot.Self.UserName)
    return bot, nil
}

func SendHourlyUpdate(ctx context.Context, bot *tgbotapi.BotAPI, channelID string) error {
    realizedPnL, _ := injectiveCache.GetRealizedPnL().Float64()
    unrealizedPnL, _ := injectiveCache.GetUnrealizedPnL().Float64()
    feeRebates, _ := injectiveCache.GetFeeRebates().Float64()
    totalGas := injectiveCache.GetTotalGas()
    message := fmt.Sprintf(
        "📊 Hourly Injective Update\nRealized PnL: %.6f USDT\nUnrealized PnL: %.6f USDT\nFee Rebates: %.6f USDT\nTotal Gas: %d",
        realizedPnL, unrealizedPnL, feeRebates, totalGas,
    )
    msg := tgbotapi.NewMessageToChannel(channelID, message)
    _, err := bot.Send(msg)
    if err != nil {
        return fmt.Errorf("failed to send Telegram message: %v", err)
    }
    log.Printf("Sent Telegram message to channel %s: %s", channelID, message)
    return nil
}