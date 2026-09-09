package games

import (
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// QuestionBank stores the curated pool of questions across all categories.
var QuestionBank = []Question{
	// ==========================================
	// 🎬 الأفلام والإفيهات المصرية (CategoryMovie)
	// ==========================================
	{
		ID:       "mov_01",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"الراجل ده هيموت متدلع!\"\nمين الفيلم صاحب الإفيه ده؟",
		AcceptedAnswers: []string{
			"التجربة الدنماركية",
			"التجربه الدنماركيه",
			"الدنماركية",
			"الدنماركيه",
		},
		Hint: "بطولة الزعيم عادل إمام ونيكول سابا.",
	},
	{
		ID:       "mov_02",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"جايلك في السريع يا فوزي!\"\nاسم الفيلم الشهير ده إيه؟",
		AcceptedAnswers: []string{
			"غبي منه فيه",
			"غبي منو في",
			"غبى منو فيه",
		},
		Hint: "هاني رمزي وحسن حسني وطلعت زكريا.",
	},
	{
		ID:       "mov_03",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"صلاح ابن عمك مات يا حزلقوم!\"\nمن أي فيلم الجملة العبقرية دي؟",
		AcceptedAnswers: []string{
			"لا تراجع ولا استسلام",
			"لا تراجع ولااستسلام",
			"حزلقوم",
		},
		Hint: "أحمد مكي ودنيا سمير غانم وماجد الكدواني.",
	},
	{
		ID:       "mov_04",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"ده أنا بابا يابني!\"\nفيلم مين الكوميدي ده؟",
		AcceptedAnswers: []string{
			"اللمبي",
			"اللمبى",
			"فيلم اللمبي",
		},
		Hint: "محمد سعد وحسن حسني وعبلة كامل.",
	},
	{
		ID:       "mov_05",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"متقولش مفيش.. قول معرفش!\"\nاسم الفيلم إيه؟",
		AcceptedAnswers: []string{
			"فول الصين العظيم",
			"فول الصين",
			"محي الشرقاوي",
		},
		Hint: "محمد هنيدي في الصين.",
	},
	{
		ID:       "mov_06",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"أنا بموت في الستات الشديدة.. شديدة إيه ده إنت اللي خفيف!\"\nإفيه من فيلم إيه؟",
		AcceptedAnswers: []string{
			"الباشا تلميذ",
			"الباشا تلميذ",
		},
		Hint: "كريم عبد العزيز وغادة عادل ورامز جلال.",
	},
	{
		ID:       "mov_07",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"مين اللي طفى النور يا ولاد؟\"\nمن فيلم إيه الجملة دي لمدرسة عاشور؟",
		AcceptedAnswers: []string{
			"الناظر",
			"الناظر صلاح الدين",
		},
		Hint: "علاء ولي الدين واللمبي وعاطف.",
	},
	{
		ID:       "mov_08",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"الله يرحمك يا عم مجاهد.. كان راجل طيب!\"\nمن أشهر إفيهات فيلم إيه؟",
		AcceptedAnswers: []string{
			"صعيدي في الجامعة الأمريكية",
			"صعيدي في الجامعة الامريكية",
			"صعيدي في الجامعه الامريكيه",
		},
		Hint: "خلف الدهشوري خلف في الجامعة الأمريكية.",
	},
	{
		ID:       "mov_09",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"يا علي إنت نمت؟ .. لسه يا كابتن!\"\nفيلم إيه اللي فيه علي الصغير وسلطان؟",
		AcceptedAnswers: []string{
			"أبو علي",
			"ابو علي",
			"ابو على",
		},
		Hint: "كريم عبد العزيز ومنى زكي وطلعت زكريا.",
	},
	{
		ID:       "mov_10",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"أنا مش قصير أوزعة.. أنا طويل وأهبل!\"\nفيلم مين الكوميدي الشهير؟",
		AcceptedAnswers: []string{
			"الناظر",
			"فيلم الناظر",
		},
		Hint: "عاطف وصلاح ولي الدين.",
	},
	{
		ID:       "mov_11",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"هات صباع موز للأستاذ عشان يهدى!\"\nمن فيلم إيه الجملة دي؟",
		AcceptedAnswers: []string{
			"طير إنت",
			"طير انت",
		},
		Hint: "دكتور بهيج والمارد ماردوجان.",
	},
	{
		ID:       "mov_12",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"الكونغو الديمقراطية!\"\nعاطف قال الجملة دي في مسابقة أوائل الطلبة بفيلم إيه؟",
		AcceptedAnswers: []string{
			"الناظر",
			"فيلم الناظر",
		},
		Hint: "أحمد حلمي بدور عاطف.",
	},
	{
		ID:       "mov_13",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"الفاتحة على روح المرحوم.. مين اللي مات؟\"\nفيلم كوميدي فانتازي إيه؟",
		AcceptedAnswers: []string{
			"سمير وشهير وبهير",
			"سمير و شهير و بهير",
		},
		Hint: "شيكو وأحمد فهمي وهشام ماجد وآلة الزمن.",
	},
	{
		ID:       "mov_14",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"أنا لو مت هتدفنوني بالبدلة دي؟\"\nأحمد حلمي قالها في فيلم إيه؟",
		AcceptedAnswers: []string{
			"مطب صناعي",
			"مطب صناعى",
		},
		Hint: "ميمي ميمي ونور والبدلة الشيك.",
	},
	{
		ID:       "mov_15",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"شرباتات يا حنفي.. شرباتات!\"\nمن روائع السينما الكلاسيكية لفيلم إيه؟",
		AcceptedAnswers: []string{
			"ابن حميدو",
			"ابن حميدو",
		},
		Hint: "إسماعيل ياسين وأحمد رمزي وزينات صدقي.",
	},
	{
		ID:       "mov_16",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"الحلزونة يما الحلزونة.. مالها الحلزونة بتزحف زحف؟\"\nإفيه عادل إمام الشهير من فيلم إيه؟",
		AcceptedAnswers: []string{
			"مرجان أحمد مرجان",
			"مرجان احمد مرجان",
			"مرجان",
		},
		Hint: "الشاعر مرجان أحمد مرجان والجامعة.",
	},
	{
		ID:       "mov_17",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"أنا رئيس جمهورية نفسي!\"\nأحمد السقا بدور مين وإسم الفيلم إيه؟",
		AcceptedAnswers: []string{
			"إبراهيم الأبيض",
			"ابراهيم الابيض",
			"ابراهيم ابيض",
		},
		Hint: "أحمد السقا ومحمود عبد العزيز (عبد الملك زرزور).",
	},
	{
		ID:       "mov_18",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"يا باسط الخلجان.. يا مسهل الجريان!\"\nمن فيلم أحمد حلمي الشهير إيه؟",
		AcceptedAnswers: []string{
			"عسل أسود",
			"عسل اسود",
			"مصري سيد العربي",
		},
		Hint: "مصري لما نزل مصر وضاع منه الباسبور.",
	},
	{
		ID:       "mov_19",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"كلنا فاسدون.. لا أستثني أحداً!\"\nأحمد زكي في مرافعته التاريخية بفيلم إيه؟",
		AcceptedAnswers: []string{
			"ضد الحكومة",
			"ضد الحكومه",
		},
		Hint: "المحامي مصطفى خلف وحادثة أتوبيس المدارس.",
	},
	{
		ID:       "mov_20",
		Category: CategoryMovie,
		Prompt:   "🎬 *إفيه وفيلم:*\n\"كان نفسي أكون رومانتيك بس الدنيا غدارة!\"\nأحمد حلمي في فيلم الشخصيات الثلاثة إيه؟",
		AcceptedAnswers: []string{
			"كدة رضا",
			"كده رضا",
			"كده رضاء",
		},
		Hint: "سمسم وبيبو وبرنس.",
	},

	// ==========================================
	// ⚽ الثقافة والمعلومات العامة (CategoryTrivia)
	// ==========================================
	{
		ID:       "tri_01",
		Category: CategoryTrivia,
		Prompt:   "⚽ *كورة ورياضة:*\nمن هو الهداف التاريخي لمنتخب مصر الأول لكرة القدم؟",
		AcceptedAnswers: []string{
			"حسام حسن",
			"العميد حسام حسن",
			"كابتن حسام حسن",
		},
		Hint: "العميد ومدرب منتخب مصر الحالي.",
	},
	{
		ID:       "tri_02",
		Category: CategoryTrivia,
		Prompt:   "🌍 *جغرافيا:*\nما هي أكبر قارات العالم من حيث المساحة وعدد السكان؟",
		AcceptedAnswers: []string{
			"آسيا",
			"اسيا",
			"قارة اسيا",
			"قارة آسيا",
		},
		Hint: "فيها الصين والهند.",
	},
	{
		ID:       "tri_03",
		Category: CategoryTrivia,
		Prompt:   "🪐 *علوم وفضاء:*\nما هو الكوكب الملقب بـ \"الكوكب الأحمر\" في مجموعتنا الشمسية؟",
		AcceptedAnswers: []string{
			"المريخ",
			"كوكب المريخ",
		},
		Hint: "رابع كواكب المجموعة الشمسية.",
	},
	{
		ID:       "tri_04",
		Category: CategoryTrivia,
		Prompt:   "🌈 *معلومات عامة:*\nكم عدد ألوان قوس قزح الرئيسية؟ (اكتب الرقم)",
		AcceptedAnswers: []string{
			"7",
			"سبعة",
			"سبع",
		},
		Hint: "رقم فردي تحت العشرة.",
	},
	{
		ID:       "tri_05",
		Category: CategoryTrivia,
		Prompt:   "🏆 *كورة وأفريقيا:*\nما هو النادي الأكثر تتويجاً بلقب دوري أبطال أفريقيا عبر التاريخ؟",
		AcceptedAnswers: []string{
			"الأهلي",
			"الاهلي",
			"النادي الأهلي",
			"نادي الاهلي",
		},
		Hint: "نادي القرن الأفريقي في الجزيرة.",
	},
	{
		ID:       "tri_06",
		Category: CategoryTrivia,
		Prompt:   "🏛️ *تاريخ:*\nمن هو الملك الفرعوني الذي أمر بنحت تمثال أبو الهول بالجيزة؟",
		AcceptedAnswers: []string{
			"خفرع",
			"الملك خفرع",
		},
		Hint: "صاحب الهرم الأوسط.",
	},
	{
		ID:       "tri_07",
		Category: CategoryTrivia,
		Prompt:   "⚡ *طبيعة وعالم الحيوان:*\nما هو أسرع حيوان بري على وجه الأرض في الركض؟",
		AcceptedAnswers: []string{
			"الفهد",
			"الشيتا",
			"فهد",
			"شيتا",
		},
		Hint: "سرعته بتعدي 110 كم/س.",
	},
	{
		ID:       "tri_08",
		Category: CategoryTrivia,
		Prompt:   "💰 *اقتصاد:*\nما هي العملة الرسمية لدولة اليابان؟",
		AcceptedAnswers: []string{
			"الين",
			"ين",
			"الين الياباني",
		},
		Hint: "حرفين أولهم ياء.",
	},
	{
		ID:       "tri_09",
		Category: CategoryTrivia,
		Prompt:   "🌊 *جغرافيا وبحار:*\nما هو أكبر محيط في العالم من حيث المساحة؟",
		AcceptedAnswers: []string{
			"المحيط الهادئ",
			"المحيط الهادي",
			"الهادئ",
			"الهادي",
		},
		Hint: "اسمه يوحي بالسكينة والهدوء.",
	},
	{
		ID:       "tri_10",
		Category: CategoryTrivia,
		Prompt:   "🎨 *فنون:*\nمن هو الرسام الإيطالي العبقري صاحب لوحة الموناليزا الشهيرة؟",
		AcceptedAnswers: []string{
			"ليوناردو دافنشي",
			"دافنشي",
			"ليوناردو دا فينشي",
		},
		Hint: "عالم وفنان عصر النهضة.",
	},
	{
		ID:       "tri_11",
		Category: CategoryTrivia,
		Prompt:   "🔬 *علوم:*\nما هو المعدن الوحيد الذي يوجد في حالة سائلة في درجة الحرارة العادية؟",
		AcceptedAnswers: []string{
			"الزئبق",
			"زئبق",
		},
		Hint: "يستخدم في موازين الحرارة القديمة (الترمومتر).",
	},
	{
		ID:       "tri_12",
		Category: CategoryTrivia,
		Prompt:   "⚽ *رياضة عالمية:*\nمن هو اللاعب الفائز بجائزة الكرة الذهبية (Ballon d'Or) لعام 2024؟",
		AcceptedAnswers: []string{
			"رودري",
			"رودريغو",
			"رودري هيرنانديز",
		},
		Hint: "نجم وسط مانشستر سيتي ومنتخب إسبانيا.",
	},
	{
		ID:       "tri_13",
		Category: CategoryTrivia,
		Prompt:   "📖 *أدب وثقافة:*\nمن هو الأديب المصري الحائز على جائزة نوبل في الأدب عام 1988؟",
		AcceptedAnswers: []string{
			"نجيب محفوظ",
			"محفوظ",
		},
		Hint: "صاحب الثلاثية وأولاد حارتنا والحرافيش.",
	},
	{
		ID:       "tri_14",
		Category: CategoryTrivia,
		Prompt:   "🫀 *طب وجسم الإنسان:*\nكم عدد حجرات قلب الإنسان؟ (اكتب الرقم)",
		AcceptedAnswers: []string{
			"4",
			"أربعة",
			"اربعة",
			"أربع",
			"اربع",
		},
		Hint: "أذينين وبطينين.",
	},
	{
		ID:       "tri_15",
		Category: CategoryTrivia,
		Prompt:   "📍 *عواصم:*\nما هي عاصمة أستراليا؟ (ركز ومش سيدني!)",
		AcceptedAnswers: []string{
			"كانبرا",
			"كانبيرا",
		},
		Hint: "بتبدأ بحرف الكاف.",
	},

	// ==========================================
	// 🧩 الفوازير وألغاز الذكاء (CategoryRiddle)
	// ==========================================
	{
		ID:       "rid_01",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة سريعة:*\nحاجة كل ما تاخد منها تكبر أكتر.. إيه هي؟",
		AcceptedAnswers: []string{
			"الحفرة",
			"الحفره",
			"حفرة",
			"حفره",
		},
		Hint: "موجودة في الأرض لما تحفر.",
	},
	{
		ID:       "rid_02",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة ذكاء:*\nحاجة بتسمع بلا ودان وبتتكلم بلا لسان.. إيه هي؟",
		AcceptedAnswers: []string{
			"التليفون",
			"الموبايل",
			"الهاتف",
			"تليفون",
			"موبايل",
		},
		Hint: "كلنا ماسكينه في إيدينا دلوقتي!",
	},
	{
		ID:       "rid_03",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nابن مامتك وباباك بس مش أخوك ولا أختك.. مين هو؟",
		AcceptedAnswers: []string{
			"أنا",
			"انا",
			"نفسي",
		},
		Hint: "فكر في نفسك إنت شخصياً!",
	},
	{
		ID:       "rid_04",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة بتمشي وتوقف بس ملهاش أي رجلين ولا بتتحرك من مكانها؟",
		AcceptedAnswers: []string{
			"الساعة",
			"الساعه",
			"ساعة",
			"ساعه",
		},
		Hint: "بتعرف منها الوقت وعندها عقارب.",
	},
	{
		ID:       "rid_05",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nكله خروق وثقوب ورغم كدة بيحفظ المية جواه.. إيه هو؟",
		AcceptedAnswers: []string{
			"الإسفنج",
			"الاسفنج",
			"اسفنج",
			"إسفنجة",
			"اسفنجه",
		},
		Hint: "بنغسل بيه المواعين والسيارات.",
	},
	{
		ID:       "rid_06",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة بتملك أسنان كتير جداً بس مستحيل تعضك.. إيه هي؟",
		AcceptedAnswers: []string{
			"المشط",
			"مشط",
			"مشط الشعر",
		},
		Hint: "بتسرح بيه شعرك كل يوم الصبح.",
	},
	{
		ID:       "rid_07",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة بتولد كل أول شهر وبتموت في آخره.. إيه هو؟",
		AcceptedAnswers: []string{
			"الهلال",
			"القمر",
			"قمر",
			"هلال",
		},
		Hint: "بينور في السما بالليل.",
	},
	{
		ID:       "rid_08",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nأخضر في الأرض، وأسود في السوق، وأحمر في البيت.. إيه هو؟",
		AcceptedAnswers: []string{
			"الشاي",
			"شاي",
		},
		Hint: "المشروب الرسمي للمصريين بعد الأكل.",
	},
	{
		ID:       "rid_09",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة مليانة مفاتيح بس متقدرش تفتح بيها أي باب في الدنيا؟",
		AcceptedAnswers: []string{
			"البيانو",
			"الاورج",
			"الأورج",
			"بيانو",
			"كيبورد",
			"لوحة المفاتيح",
		},
		Hint: "آلة موسيقية مشهورة مفاتيحها بيضا وسودا.",
	},
	{
		ID:       "rid_10",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nطائر يلد ولا يبيض ويرضع صغاره.. مين هو؟",
		AcceptedAnswers: []string{
			"الخفاش",
			"خفاش",
			"الوطواط",
			"وطواط",
		},
		Hint: "بيطير بالليل وينام بالمقلوب (باتمان).",
	},
	{
		ID:       "rid_11",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة كل ما تسلقها أو تسخنها على النار تجمد وتصلب؟",
		AcceptedAnswers: []string{
			"البيض",
			"البيضة",
			"البيضه",
			"بيض",
		},
		Hint: "بناكله مسلوق في الفطار.",
	},
	{
		ID:       "rid_12",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة بتمشي وراك طول النهار في الشمس وبتختفي فوراً بالليل؟",
		AcceptedAnswers: []string{
			"الظل",
			"خيال",
			"خيالك",
			"ضلك",
			"ظلك",
		},
		Hint: "ضلك وخيالك.",
	},
	{
		ID:       "rid_13",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة بتشوف كل حاجة حواليها بس ملهاش أي عيون؟",
		AcceptedAnswers: []string{
			"المراية",
			"المرايه",
			"المرآة",
			"مراية",
		},
		Hint: "بتبص فيها عشان تشوف شكلك.",
	},
	{
		ID:       "rid_14",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة بتكسي وتلبس كل الناس وهي بتفضل عريانة؟",
		AcceptedAnswers: []string{
			"الإبرة",
			"الابرة",
			"الابره",
			"إبرة",
			"ابره",
			"إبرة الخياطة",
		},
		Hint: "بتستخدمها مع الخيط في الخياطة.",
	},
	{
		ID:       "rid_15",
		Category: CategoryRiddle,
		Prompt:   "🧩 *فزورة:*\nحاجة كل ما زادت ونمت فيها نقصت من عمرك؟",
		AcceptedAnswers: []string{
			"العمر",
			"عمر",
			"السن",
			"سنك",
		},
		Hint: "سنين حياتك.",
	},
}

// GetRandomQuestions selects n non-repeating questions filtered by category.
// If category is CategoryMixed or empty, it picks randomly across all categories.
func GetRandomQuestions(cat Category, count int) []Question {
	var pool []Question
	if cat == "" || cat == CategoryMixed {
		pool = append(pool, QuestionBank...)
	} else {
		for _, q := range QuestionBank {
			if q.Category == cat {
				pool = append(pool, q)
			}
		}
	}

	if len(pool) == 0 {
		pool = append(pool, QuestionBank...)
	}

	// Shuffle
	shuffled := make([]Question, len(pool))
	copy(shuffled, pool)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	if count > len(shuffled) {
		count = len(shuffled)
	}
	return shuffled[:count]
}
