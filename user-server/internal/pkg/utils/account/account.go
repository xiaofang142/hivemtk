package account

import (
	"context"
	"errors"
	"fmt"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tgbot"
	logger "hivemtk-user/internal/pkg/utils/logger"
	_type "hivemtk-user/internal/pkg/utils/type"
	"hivemtk-user/internal/service"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/shopspring/decimal"
)

var StartCommandString = "/start"

type accountData struct {
	BotToken    string
	Price       decimal.Decimal
	AccountID   string
	GroupID     int64
	AccountName string
	Bot         *tgbotapi.BotAPI
	BotUrl      string
}

var (
	globalAccounts = make(map[string]accountData)
	dictLock       sync.RWMutex
)

func InitAllAccount() error {
	accountSer := service.NewAccountService()

	accountList, err := accountSer.GetAccountList(context.Background())
	if err != nil {
		return err
	}
	for _, account := range accountList {
		botName, err := InitOneAccount(account)
		if err != nil {
			logger.Info(fmt.Sprintf("初始化账号ID %s 失败: %s", account.ID, err.Error()))
			if e := accountSer.UpdateAccountStatusById(context.Background(), account.ID, _type.AccountStatusInactive, err.Error()); e != nil {
				logger.Warnf("更新账号失活状态失败 ID=%s: %v", account.ID, e)
			}
		} else {
			logger.Info(fmt.Sprintf("初始化账号ID %s 成功: %s", account.ID, botName))
			if e := accountSer.UpdateAccountStatusById(context.Background(), account.ID, _type.AccountStatusActive, ""); e != nil {
				logger.Warnf("更新账号激活状态失败 ID=%s: %v", account.ID, e)
			}
			if e := accountSer.UpdateAccountTgNameById(context.Background(), account.ID, botName); e != nil {
				logger.Warnf("更新账号 TgName 失败 ID=%s: %v", account.ID, e)
			}
		}
	}
	return nil
}

func InitOneAccount(account *model.Account) (string, error) {
	bot, err := tgbot.InitTGBot(account.TgBotToken, account.GroupID, account.ProxyEnableProxy, account.ProxyProtoclo, account.ProxyHost, account.ProxyPort)
	if err != nil {
		return "", err
	}
	botName := bot.Self.FirstName
	account.TgName = botName

	accountData, err := FormateAccountDictData(account, bot)
	accountData.AccountName = botName
	if err != nil {
		return botName, err
	}
	SetAccount(account.TgBotToken, accountData)
	go RunTgBot(accountData)
	return botName, nil
}

func FormateAccountDictData(account *model.Account, bot *tgbotapi.BotAPI) (accountData, error) {
	price, err := decimal.NewFromString(account.Price)
	if err != nil {
		return accountData{}, err
	}
	info := accountData{
		BotToken:    account.TgBotToken,
		Price:       price,
		AccountID:   account.ID,
		GroupID:     account.GroupID,
		AccountName: account.TgName,
		Bot:         bot,
		BotUrl:      account.URL,
	}
	return info, nil
}

func GetAccount(token string) (accountData, error) {
	dictLock.RLock()
	defer dictLock.RUnlock()
	accountData, ok := globalAccounts[token]
	if !ok {
		return accountData, errors.New("bot not found")
	}
	return accountData, nil
}

func SetAccount(token string, account accountData) {
	dictLock.Lock()
	defer dictLock.Unlock()
	globalAccounts[token] = account
}

func RunTgBot(account accountData) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := account.Bot.GetUpdatesChan(u)

	for update := range updates {
		go handleUpdate(account, update)
	}
}

func handleUpdate(account accountData, update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(fmt.Errorf("机器人处理消息崩溃: %v", r), "goroutine panic recovered")
		}
	}()

	var from_id = update.Message.From.ID
	var from_FirstName = update.Message.From.FirstName
	var from_LastName = update.Message.From.LastName
	var from_UserName = update.Message.From.UserName
	var text = update.Message.Text

	userSer := service.NewUserService()
	user_id, err := userSer.InitUser(context.Background(), account.AccountID, int64(from_id), from_FirstName, from_LastName, from_UserName)
	if err != nil {
		logger.Info(err.Error())
	}
	messageSer := service.NewMessageService()
	_, err = messageSer.InitMessage(context.Background(), account.AccountID, user_id, int64(from_id), text)
	if err != nil {
		logger.Info(err.Error())
	}

	if update.Message != nil && update.Message.Chat.IsPrivate() {
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				msgText := BuildAccountStartNoticeMsg(account.AccountName)
				if e := tgbot.SendTgMsg(account.Bot, msgText, update.Message.Chat.ID); e != nil {
					logger.Warnf("发送机器人通知失败 chatID=%d: %v", update.Message.Chat.ID, e)
				}
			default:
				msgText := BuildAccountStartNoticeMsg(account.AccountName)
				if e := tgbot.SendTgMsg(account.Bot, msgText, update.Message.Chat.ID); e != nil {
					logger.Warnf("发送机器人通知失败 chatID=%d: %v", update.Message.Chat.ID, e)
				}
			}
		} else {
			msgText := BuildAccountStartNoticeMsg(account.AccountName)
			if e := tgbot.SendTgMsg(account.Bot, msgText, update.Message.Chat.ID); e != nil {
				logger.Warnf("发送机器人通知失败 chatID=%d: %v", update.Message.Chat.ID, e)
			}
		}
	}

	if update.Message.NewChatMembers != nil {
		msgText := BuildAccountStartNoticeMsg(account.AccountName)
		if e := tgbot.SendTgMsg(account.Bot, msgText, update.Message.Chat.ID); e != nil {
			logger.Warnf("发送机器人通知失败 chatID=%d: %v", update.Message.Chat.ID, e)
		}
	}
}

func BuildAccountStartNoticeMsg(TgName string) string {
	msgText := fmt.Sprintf("欢迎使用: %s", TgName)
	return msgText
}

func SendMsgBYBootToken(token string, msgText string, chatID int64) error {
	account, err := GetAccount(token)
	if err != nil {
		return err
	}
	return tgbot.SendTgMsg(account.Bot, msgText, chatID)
}
