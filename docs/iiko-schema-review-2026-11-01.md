# Сверка адаптера меню с официальной схемой — 1 ноября 2026

Источник: публичный GET [OpenAPI iikoCloud](https://api-ru.iiko.services/api-docs/docs).
Получен HTTP 200, application/json, OpenAPI 3.0.4, размер 2333936 байт.
SHA-256: `022ffac44d581f981a76408c426bd30c3393ff0c5333c7b3d40629dc26bbffb7`. Запрос без credentials, API ресторана не вызывался.
Полная копия сохранена локально рядом с интеграционным worktree, в Git не включена.

| Поле/метод | Что подтверждено | Изменение |
|---|---|---|
| /api/1/nomenclature | Существует, deprecated; рекомендован api/menu/v3/by_id | Существующий источник сохранён; миграция отдельно |
| startRevision | 0 для полного первого запроса; равенство revision означает пустые данные | Каждый раз запрашиваем полную версию с 0 |
| ProductInfo.type | dish, good, modifier | Поддержаны dish/good без учёта регистра |
| useBalanceForSell | Обязательный boolean, признак весового продукта | true/отсутствие блокируют продажу |
| canSetOpenPrice | Обязательный boolean, открытая цена | true/отсутствие блокируют продажу |
| orderItemType | Product или Compound | Compound/неизвестный тип блокируются |
| sizePrices | sizeId (nullable), price.currentPrice, price.isIncludedInMenu | Одна цена без размера, положительная и включённая |
| /api/1/stop_lists request | organizationIds, **terminalGroupsIds**, returnSize | Исправлено прежнее terminalGroupIds в Go и Python |
| stop response | terminalGroupStopLists → organizationId/items → terminalGroupId/items → productId/balance | Имеющаяся вложенность соответствует схеме |

Fixtures синтетические; ключевые случаи закреплены TestOfficialProductSaleContract
и проверкой HTTP request в menusync. Адаптер намеренно строже общей схемы:
неполный активный продукт/нет isDeleted/цен — ошибка снимка; размеры, модификаторы,
вес, открытая цена и compound не продаются. Сбой не превращается в пустой стоп-лист.

**Не доказано:** поддержка deprecated endpoint конкретной лицензией, полнота
выгрузки ресторана, допустимая частота, число реально исключённых блюд, корректность
ценовой категории и фактическая доступность на кассе. Нужен согласованный read-only
ответ меню/стопов и ревью исключённых позиций. Подключение внешнего меню v3,
вебхуков или изменение внешних настроек в эту интеграцию не входят.
