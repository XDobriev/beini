package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"beini-backend/internal/config"
	"beini-backend/internal/iiko"
)

func main() {
	fmt.Println("============================================================")
	fmt.Println("  ЗАПУСК ТЕСТОВОГО ЗАКАЗА В РЕСТОРАН BEINI (GOLANG КЛИЕНТ)")
	fmt.Println("============================================================")

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn, // Минимизируем лишний лог при консольном тесте
	}))

	cfg, err := config.Load("iiko_config.json")
	if err != nil {
		fmt.Printf("Ошибка загрузки конфигурации: %v\n", err)
		os.Exit(1)
	}

	client := iiko.NewClient(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	fmt.Println("\n1. Проверка авторизации в iikoCloud (API v2)...")
	token, err := client.GetToken(ctx)
	if err != nil {
		fmt.Printf("Ошибка получения токена: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Токен получен: %s... (длина: %d симв.)\n", token[:20], len(token))

	fmt.Printf("\n2. Отправка заказа на кассу ресторана (терминал: %s)...\n", cfg.TerminalGroupID)
	res, err := client.SendTestOrder(ctx)
	if err != nil {
		fmt.Printf("Ошибка отправки заказа: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Ответ сервера iikoCloud получен!")
	fmt.Printf("  Correlation ID (ID очереди): %s\n", res.CorrelationID)
	fmt.Printf("  Статус создания в iiko: %s\n", res.OrderInfo.CreationStatus)

	if res.OrderInfo.CreationStatus == "Success" || res.OrderInfo.CreationStatus == "InProgress" {
		fmt.Println("\n============================================================")
		fmt.Println("✓ ЗАКАЗ УСПЕШНО ПЕРЕДАН НА КАССУ iikoFront!")
		fmt.Println("  Проверьте терминал: звуковой сигнал / вкладка 'Доставка'.")
		fmt.Println("============================================================")
	} else if res.OrderInfo.CreationStatus == "Error" {
		errInfo := res.OrderInfo.ErrorInfo
		if errInfo != nil {
			fmt.Printf("\n[!] Касса вернула ошибку: %s (%s)\n", errInfo.Code, errInfo.Message)
			if errInfo.Code == "TerminalGroupDisabled" {
				fmt.Println("  Причина: Кассовый терминал в ресторане сейчас выключен или смена закрыта.")
				fmt.Println("  В пятницу в рабочее время при включенном терминале заказ сразу перейдет в Success.")
			}
		}
		fmt.Println("============================================================")
	}
}

