// Меню ресторана BEINI (Назрань) со структурой номенклатуры iiko
const MENU_DATA = [
  {
    category: "grill",
    categoryName: "Мясо на углях & Гриль",
    categoryIcon: "flame",
    items: [
      {
        id: "beini-grill-01",
        iikoId: "iiko-meat-0101",
        name: "Фирменное ассорти на огне BEINI",
        description: "Сочные шашлыки из баранины, нежная телятина, люля-кебаб и куриное филе на углях. Подается с маринованным луком, зеленью и домашним соусом.",
        price: 1850,
        weight: "750 г",
        image: "assets/images/instagram/dish_meat_18.jpg",
        badge: "Хит ресторана",
        isHalal: true
      },
      {
        id: "beini-grill-02",
        iikoId: "iiko-meat-0102",
        name: "Шашлык из отборной мякоти ягненка",
        description: "Традиционный кавказский рецепт маринования. Мясо тает во рту, подается с тонким лавашем и гранатовыми зернами.",
        price: 680,
        weight: "220 г",
        image: "assets/images/instagram/dish_meat_16.jpg",
        badge: "Выбор шефа",
        isHalal: true
      },
      {
        id: "beini-grill-03",
        iikoId: "iiko-meat-0103",
        name: "Люля-кебаб из телятины с кинзой",
        description: "Рубленое мясо с восточными пряностями, приготовленное на углях. Сочное и ароматное блюдо.",
        price: 490,
        weight: "200 г",
        image: "assets/images/instagram/reel_10.jpg",
        badge: "Традиции",
        isHalal: true
      },
      {
        id: "beini-grill-04",
        iikoId: "iiko-meat-0104",
        name: "Стейк Рибай с овощами гриль",
        description: "Премиальная мраморная говядина зернового откорма, обжаренная до нужной степени прожарки со сливочно-перечным соусом.",
        price: 1350,
        weight: "320 г",
        image: "assets/images/instagram/dish_meat_18.jpg",
        badge: "Премиум",
        isHalal: true
      }
    ]
  },
  {
    category: "soups",
    categoryName: "Супы & Национальное",
    categoryIcon: "soup",
    items: [
      {
        id: "beini-soup-01",
        iikoId: "iiko-soup-0201",
        name: "Тайский суп Том-Ям",
        description: "Насыщенный пряный бульон на кокосовом молоке с тигровыми креветками, кальмарами, грибами цаогу, черри и рисом жасмин.",
        price: 580,
        weight: "350 г",
        image: "assets/images/instagram/reel_11.jpg",
        badge: "Популярное",
        isHalal: true
      },
      {
        id: "beini-soup-02",
        iikoId: "iiko-soup-0202",
        name: "Наваристая шурпа из баранины",
        description: "Классический горный суп долгого томления с крупными кусками баранины, картофелем, нутом и горными травами.",
        price: 480,
        weight: "400 г",
        image: "assets/images/instagram/dish_meat_16.jpg",
        badge: "Халяль",
        isHalal: true
      },
      {
        id: "beini-soup-03",
        iikoId: "iiko-soup-0203",
        name: "Узбекский лагман ручной работы",
        description: "Тянутая вручную пшеничная лапша с ароматной подливой из молодой говядины, болгарского перца и сельдерея.",
        price: 450,
        weight: "380 г",
        image: "assets/images/instagram/reel_12.jpg",
        badge: "Сытно",
        isHalal: true
      }
    ]
  },
  {
    category: "salads",
    categoryName: "Салаты & Закуски",
    categoryIcon: "salad",
    items: [
      {
        id: "beini-salad-01",
        iikoId: "iiko-salad-0301",
        name: "Салат с авокадо и свежими томатами",
        description: "Спелый авокадо, розовые бакинские томаты, микс свежей зелени, кедровые орешки и фирменная оливково-лаймовая заправка.",
        price: 595,
        weight: "300 г",
        image: "assets/images/instagram/dish_salad_14.jpg",
        badge: "Сезонное",
        isHalal: true
      },
      {
        id: "beini-salad-02",
        iikoId: "iiko-salad-0302",
        name: "Хрустящие баклажаны со страчателлой",
        description: "Хрустящие ломтики баклажанов в карамельном сладком чили, нежная страчателла, спелые мандарины и кунжут.",
        price: 540,
        weight: "260 г",
        image: "assets/images/instagram/reel_13.jpg",
        badge: "Бестселлер",
        isHalal: true
      },
      {
        id: "beini-salad-03",
        iikoId: "iiko-salad-0303",
        name: "Цезарь с тигровыми креветками",
        description: "Хрустящий романо, обжаренные на чесночном масле тигровые креветки, пармезан, домашние крутоны и соус цезарь.",
        price: 620,
        weight: "250 г",
        image: "assets/images/instagram/dish_salad_14.jpg",
        badge: "Классика",
        isHalal: true
      }
    ]
  },
  {
    category: "sushi",
    categoryName: "Суши & Паназия",
    categoryIcon: "utensils",
    items: [
      {
        id: "beini-sushi-01",
        iikoId: "iiko-roll-0401",
        name: "Фирменный ролл BEINI",
        description: "Опаленный лосось, сливочный сыр, снежный краб, авокадо, соус унаги и хрустящий миндаль.",
        price: 690,
        weight: "8 шт / 280 г",
        image: "assets/images/instagram/reel_15.jpg",
        badge: "Авторский",
        isHalal: true
      },
      {
        id: "beini-sushi-02",
        iikoId: "iiko-roll-0402",
        name: "Филадельфия Премиум с лососем",
        description: "Щедрый слой свежего слабосоленого лосося, нежный сыр креметте, свежий огурец и японский рис.",
        price: 650,
        weight: "8 шт / 260 г",
        image: "assets/images/instagram/reel_17.jpg",
        badge: "Любимое",
        isHalal: true
      }
    ]
  },
  {
    category: "drinks",
    categoryName: "Авторский бар & Кофе",
    categoryIcon: "coffee",
    items: [
      {
        id: "beini-drink-01",
        iikoId: "iiko-bar-0501",
        name: "Айс-кофе с ингушским урбечом",
        description: "Знаменитый локальный хит: двойной шот арабики, свежее молоко, лед и натуральный урбеч из семян льна. Неповторимый орехово-карамельный вкус.",
        price: 350,
        weight: "300 мл",
        image: "assets/images/instagram/reel_19.jpg",
        badge: "Локальный хит",
        isHalal: true
      },
      {
        id: "beini-drink-02",
        iikoId: "iiko-bar-0502",
        name: "Горный авторский чай с травами и ягодами",
        description: "Чайный сбор из горных трав Джейраха, сушеные ягоды облепихи, мята, мед и цитрусовые нотки.",
        price: 390,
        weight: "800 мл",
        image: "assets/images/instagram/reel_20.jpg",
        badge: "Традиции",
        isHalal: true
      },
      {
        id: "beini-drink-03",
        iikoId: "iiko-bar-0503",
        name: "Цитрусовый лимонад Маракуйя-Мята",
        description: "Освежающий натуральный лимонад на основе пюре спелой маракуйи, свежевыжатого сока лайма и содовой.",
        price: 320,
        weight: "400 мл",
        image: "assets/images/instagram/reel_21.jpg",
        badge: "Освежает",
        isHalal: true
      }
    ]
  }
];
