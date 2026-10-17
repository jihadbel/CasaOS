# التقرير التفصيلي الشامل لفحص النظام والتغييرات وجاهزية الإطلاق التجاري
**التاريخ:** 17 أكتوبر 2026  
**المشروع:** CasaOS (نسخة الخادم المستقل وسحابة الملفات والصور — Windows & Linux)  
**نطاق الفحص:** الكود المصدري الكامل، التغييرات الأخيرة في شجرة العمل (`21 ملفاً معدلاً + 8 ملفات جديدة`)، الالتزامات الأخيرة، الواجهة الأمامية (`web/index.html`)، سكربتات التثبيت وSMB، وحزم الاختبارات الآلية.

---

## 1. الملخص التنفيذي وقرار جاهزية الإطلاق التجاري

> [!WARNING]
> **قرار الجاهزية للإطلاق التجاري:** **غير جاهز للإطلاق التجاري العام (NO-GO for Public Commercial Launch)** في حالته الراهنة.  
> **الجاهزية للإطلاق التجريبي المقيد (Private Beta):** **قريب جداً (Conditional GO)** فور إغلاق البنود الحرجة والعالية (P0 / P1) الموثقة في هذا التقرير.

### ملخص نتائج الاختبارات الحالية على النظام
| الفحص | النتيجة الحالية | ملاحظات |
|---|---|---|
| `go test ./... -count=1 -timeout=120s` | **ناجح بالكامل** (13 حزمة مختبرة) | يشمل اختبارات الانحدار، والمصادقة، والرفع، والتنزيل، والسلة، والأرشفة، وWebSocket |
| `go vet ./...` | **ناجح بالكامل** (0 تحذيرات) | لا توجد أخطاء تحليل ثابت |
| `node --test web/index.test.cjs` | **ناجح بالكامل** (23 / 23 اختباراً) | يشمل اختبارات الواجهة، والمحرر، والمعرض، وتحميل الفيديو الكسول |
| `scripts/test-windows-smb.ps1` | **ناجح بالكامل** | محاكاة مطابقة مشاركات SMB وعزل متغيرات البيئة |
| `scripts/test-install-security.ps1` | **ناجح بالكامل** | محاكاة فحص ACL وملكية `Administrators` و`SYSTEM` |

### ملخص الأخطاء والمشاكل المكتشفة حسب درجة الخطورة
- **أخطاء حرجة (P0 — Critical):** **5 أخطاء** (قد تؤدي إلى انهيار الخادم Panic، أو SSRF، أو الكتابة خارج المسارات المسموحة، أو إيقاف نظام التشغيل).
- **أخطاء عالية الخطورة (P1 — High):** **9 أخطاء** (التفاف على حماية المسارات الداخلية `InternalStoragePath`، ثغرات في مسارات قديمة، تعارضات وظيفية، وصلاحيات SMB مفرطة).
- **أخطاء متوسطة الخطورة (P2 — Medium):** **8 أخطاء** (اختناقات أداء وتزامن، استهلاك ذاكرة، تسريب ملفات مؤقتة متروكة، وحقن JSON في WebSocket).
- **فجوات الجاهزية التجارية والمنتج (Commercial & Product Gaps):** **7 متطلبات** (التثبيت، التوقيع الرقمي، الإعداد الأولي، HTTPS التلقائي، العمل دون إنترنت، ونموذج الصلاحيات).

---

## 2. تقييم التغييرات الأخيرة (في شجرة العمل الحالية)

أُجريت في شجرة العمل الحالية تعديلات مهمة في 21 ملفاً مع إضافة 8 ملفات جديدة. الجدول التالي يوضح تقييم كل تغيير والمشاكل أو الحالات الحدية التي ظهرت فيه:

| التغيير الأخير | الملفات المعدلة | التقييم الفني | المشاكل / الثغرات المتبقية في هذا التغيير |
|---|---|---|---|
| **استخراج إعدادات التخزين وحصر المسارات** | [`pkg/storagepaths/settings.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/storagepaths/settings.go)<br>[`pkg/utils/file/storage.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/storage.go)<br>[`storage_api.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/storage_api.go)<br>[`api_extensions.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/api_extensions.go) | ممتاز من حيث منع المسارات النسبية والروابط ومنع تصفير الملفات في `CreateFile`. | 1. **تعارض وظيفي (UX Bug):** متصفح الملفات يعرض كل الأقراص (`D:\` مثلاً)، لكن إنشاء ملف جديد [`PostCreateFile`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L456-L477) يفشل بـ `400` في أي قرص غير مضاف في `storage-paths.json`، بينما إنشاء مجلد [`MkdirAll`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L427-L446) ورفع ملف [`PostFileUpload`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L544-L678) ينجحان في المكان نفسه.<br>2. إذا ضبط المستخدم `CASAOS_DATA_ROOT=E:\` (جذر قرص)، تفشل [`storagepaths.Validate`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/storagepaths/settings.go#L58-L60) لأن `DefaultPath` يصبح جذر قرص. |
| **حماية إعادة التسمية ودعم تغيير حالة الأحرف** | [`service/system.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/system.go#L214-L242)<br>[`route/v1/file.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L393-L417) | سليم ومحصّن عبر [`ValidateDeletePath`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/delete.go#L14-L36) و[`safefs.NoReplace`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/internal/safefs/rename_windows.go). | لا توجد مشاكل في مسار إعادة التسمية نفسه. |
| **تحديد سقف تنزيل المجلدات** | [`route/v1/directory_download.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/directory_download.go) | أضاف حداً بـ `10,000` عنصر وعمق `128` وقراءة دفعات `ReadDir(128)`. | 1. مسار `GET /v1/batch?files=<dir>` في [`GetDownloadFile`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L176-L222) لا يمر عبر `downloadDirectory` عندما يكون العنصر مجلداً، بل يستدعي [`file.AddFile`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/file.go#L324-L377) غير المحدود.<br>2. لا يوجد سقف للحجم الإجمالي بالبايت (Total Bytes Limit). |
| **تنظيف أجزاء الرفع عند التعارض** | [`route/v1/upload_chunks.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/upload_chunks.go#L52-L66)<br>[`route/v1/file.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L512-L595) | يحذف مجلد العملية المؤقت عند حدوث `409 Conflict`. | لا ينظف عمليات الرفع المتروكة عند إغلاق المتصفح أو انقطاع الشبكة (تبقى في `.temp` للأبد). |
| **مزامنة WebSocket** | [`route/v1/file.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L898-L1220) | أصلح سباق الخرائط (`RWMutex`) وحجب البث (`enqueue`). | بقيت ثغرة غياب `SetReadLimit`، وحقن JSON عبر `c.ID`، وطباعة Debug الصينية في الطرفية. |
| **أسماء مشاركات SMB** | [`route/v1/samba.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/samba.go)<br>[`service/shares.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/shares.go)<br>[`service/shares_windows.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/shares_windows.go) | أضاف التحقق من الأسماء ودعم تسمية جذور الأقراص `C_Drive`. | 1. لا يمنع مشاركة مجلدات الخدمة الداخلية (`InternalStoragePath`).<br>2. في `shares_windows.go` يفشل تحديث المسار إذا كانت المشاركة السابقة مملوكة لـ CasaOS نفسه.<br>3. يمنح `Everyone` صلاحية `FullAccess`. |
| **حفظ سجل السلة التدريجي عند التفريغ** | [`trash_api.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/trash_api.go#L268-L286) | يحفظ السجل بعد حذف كل عنصر لمنع تضارب الحالة عند فشل حذف ملف مقفل. | يحفظ السجل مرتين في نهاية الحلقة (السطر 278 والسطر 283). |

---

## 3. القائمة التفصيلية الكاملة بجميع الأخطاء والثغرات في النظام

### الفئة الأولى: أخطاء حرجة (P0 — Critical)

#### الخطأ #1: مسار الرفع في V2 (`POST /v2/casaos/file/upload`) يتجاوز كل الحمايات ويسبب تسريب مقابض وانهيار الخادم (Panic)
- **الموقع:** [`route/v2/file.go:21-86`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v2/file.go#L21-L86) و[`service/file_upload.go:34-167`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/file_upload.go#L34-L167) و[`route/v2.go:54-130`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v2.go#L54-L130).
- **التفاصيل الفنية:**
  1. **غياب فحص المسارات والكتابة العشوائية:** في [`UploadFile`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/file_upload.go#L79-L90) يتم بناء مسار الملف مباشرة عبر `path + "/" + relativePath + ".tmp"` دون التحقق من `filepath.IsLocal` أو `safefs.NoLinks` أو `InternalStoragePath`، ويستبدل الملفات النهائية عبر `os.Rename` (السطر 156).
  2. **تسريب مقابض الملفات وفشل النقل على ويندوز (File Descriptor Leak):** في السطر 89:
     ```go
     file, err := os.OpenFile(path+"/"+relativePath+".tmp", os.O_WRONLY|os.O_CREATE, 0644)
     ```
     لا يوجد `defer file.Close()`! يتم إغلاق `file` فقط عندما يكتمل آخر جزء `if fileInfo.uploadedChunkNum == totalChunks` (السطر 150). في جميع الأجزاء السابقة (`1..N-1`) يبقى مقبض الملف مفتوحاً في النظام، مما يمنع `os.Rename` في السطر 156 من النجاح على ويندوز لأن الملف مفتوح بمقابض سابقة لم تُغلق!
  3. **انهيار العملية بالكامل (Unrecovered Panic):** في السطر 53 (`TestChunk`) والسطر 143 (`UploadFile`):
     ```go
     if !fileInfo.uploaded[chunkNumber-1]
     ```
     لا يوجد فحص لحدود `chunkNumber` (`chunkNumber < 1 || chunkNumber > int64(len(fileInfo.uploaded))`) ولا لكون `totalChunks <= 0` أو ضخماً جداً (يمكن إرسال `totalChunks = 10000000000` لاستنزاف الذاكرة في `make([]bool, totalChunks)` بالسطر 112). والأخطر أن [`InitV2Router`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v2.go#L54-L130) **لا يضم `echo_middleware.Recover()`**، مما يعني أن أي Panic في مسارات V2 يُسقط عملية الخادم (`casaos-server.exe`) بالكامل فوراً!
- **الحل المطلوب:** إيقاف أو حذف مسار رفع V2 القديم (أو تحويله إلى معالج V1 الآمن)، وإضافة `echo_middleware.Recover()` إلى موجه V2.

---

#### الخطأ #2: انهيار الخادم (Nil Pointer Panic) في حساب حجم المجلدات `DirSizeB` عند مصادفة ملف أو مجلد فرعي يتعذر قراءته
- **الموقع:** [`pkg/utils/file/file.go:446-455`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/file.go#L446-L455)، ويُستدعى من [`route/v1/file.go:703`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L703) (`PostOperateFileOrDir`) و[`route/v1/file.go:880`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L880) (`GetSize`).
- **التفاصيل الفنية:**
  ```go
  func DirSizeB(path string) (int64, error) {
      var size int64
      err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
          if !info.IsDir() {
              size += info.Size()
          }
          return err
      })
      return size, err
  }
  ```
  عندما يفشل `os.Lstat` أثناء `filepath.Walk` على أي مجلد فرعي محمي بصلاحيات النظام (مثل `System Volume Information` أو مجلد لمستخدم آخر على ويندوز) أو ملف محذوف أثناء الفحص، يمرر `filepath.Walk` القيمة `info = nil` مع `err != nil`. استدعاء `!info.IsDir()` قبل فحص `if err != nil || info == nil` يؤدي فوراً إلى **Nil Pointer Dereference Panic**!
- **الحل المطلوب:** فحص `if err != nil { return err }` و`if info == nil { return nil }` في بداية دالة `WalkFunc`، مع وضع سقف لعمق ووقت حساب الحجم.

---

#### الخطأ #3: ثغرتا SSRF (Server-Side Request Forgery) صريحتان في `/v1/sys/proxy` و`/v1/other/search`
- **الموقع:** [`route/v1/system.go:339-356`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/system.go#L339-L356) (`GetSystemProxy`) و[`route/v1/other.go:12-28`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/other.go#L12-L28) (`GetSearchResult`) و[`service/other.go:103-112`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/other.go#L103-L112) (`AgentSearch`).
- **التفاصيل الفنية:**
  - `GET /v1/sys/proxy?url=...` يجلب أي رابط يمرره العميل عبر `http2.Get(url, 30*time.Second)` ويقرأ كامل الجسم في الذاكرة (`ioutil.ReadAll(resp.Body)`) دون حد للحجم ويعيده للمستدعي.
  - `GET /v1/other/search` يقرأ الحقل `url` من جسم الطلب ويمرره إلى `AgentSearch(url)` الذي يجلب أي عنوان URL ويعيد محتواه.
  - يتيح ذلك فحص المنافذ الداخلية للخادم، أو الوصول إلى خدمات محلية غير مصادقة (مثل `rclone` على `127.0.0.1:5572` أو خدمات الإدارة الداخلية أو عناوين Metadata السحابية `169.254.169.254`).
- **الحل المطلوب:** إزالة المسارين أو تقييدهما بقائمة بيضاء صارمة للنطاقات الخارجية المسموحة ومنع العناوين الخاصة والمحلية (Loopback / Link-Local / Private RFC1918) مع تحديد سقف لحجم الاستجابة.

---

#### الخطأ #4: قراءة وتعديل ملفات الخدمة الداخلية (`password.hash`, `casaos.conf`, `trash_manifest.json`) عبر واجهة المحرر النصي
- **الموقع:** [`route/v1/file.go:85-121`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L85-L121) (`GetFilerContent`) و[`route/v1/file.go:773-834`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L773-L834) (`PutFileContent`).
- **التفاصيل الفنية:**
  - بينما جرى تأمين مسارات التنزيل (`/v1/file`, `/v3/file`, `/v1/image`, `/v1/batch`) بمنع [`file.InternalStoragePath`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/internal_storage.go#L12-L42)، فإن مسار قراءة المحرر `GET /v1/file/content?path=...` **لا يستدعي `file.InternalStoragePath(filePath)` ولا `safefs.NoLinks(filePath)`**!
  - ومسار حفظ المحرر `PUT /v1/file` يستدعي `safefs.NoLinks(fi.FilePath)` فقط، لكنه **لا يستدعي `file.InternalStoragePath(fi.FilePath)` ولا `file.ValidateDeletePath(fi.FilePath)`**!
  - نتيجة لذلك، يمكن عبر المحرر قراءة `C:\ProgramData\CasaOS\conf\auth\password.hash` أو `initial-password.txt` أو `casaos.conf`، أو الكتابة فوق `casaos.conf` و`storage-paths.json` و`trash_manifest.json` مباشرة.
- **الحل المطلوب:** إضافة فحص `file.InternalStoragePath` و`safefs.NoLinks` إلى `GetFilerContent` و`PutFileContent`.

---

#### الخطأ #5: حلقة لا نهائية (Infinite Loop) وتسريب كلمة المرور في السجلات في `WsSsh`
- **الموقع:** [`route/v1/ssh.go:47-95`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/ssh.go#L47-L95).
- **التفاصيل الفنية:**
  1. يستقبل `WsSsh` اسم المستخدم وكلمة المرور عبر معاملات الرابط `ctx.QueryParam("password")`، مما يؤدي إلى تسجيل كلمة المرور بالنص الصريح في سجلات Echo (`echo_middleware.Logger()`).
  2. لا يتحقق من خطأ `upgrader.Upgrade` (السطر 56: `wsConn, _ := upgrader.Upgrade(...)`)؛ فإذا تم استدعاء `GET /v1/sys/wsssh` كطلب HTTP عادي (بدون ترويسات WebSocket)، تكون `wsConn == nil` ويحدث **Nil Pointer Dereference Panic** عند السطر 70 أو 75!
  3. في الأسطر 66-81:
     ```go
     for login != 0 {
         if userName == "" || password == "" || port == "" {
             wsConn.WriteMessage(websocket.TextMessage, []byte("username or password or port is empty"))
         }
         client, err = sshHelper.NewSshClient(userName, password, port)
         if err != nil && client == nil {
             wsConn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
             ...
         } else {
             login = 0
         }
     }
     ```
     قيم `userName` و`password` و`port` لا تتغير أبداً داخل الحلقة! إذا كانت فارغة أو خاطئة، تدور الحلقة إلى ما لا نهاية بأقصى سرعة للمعالج (100% CPU Core + محاولات اتصال SSH غير منتهية) ولا تتوقف حتى بعد إغلاق العميل للاتصال!
- **الحل المطلوب:** تعطيل مسار SSH في النسخة المستقلة، أو إزالة الحلقة اللانهائية، وفحص خطأ `upgrader.Upgrade`، وعدم تمرير كلمة المرور في Query String.

---

### الفئة الثانية: أخطاء عالية الخطورة (P1 — High)

#### الخطأ #6: إمكانية نسخ أو نقل ملفات الخدمة الداخلية وجذور التخزين عبر `POST /v1/batch/task`
- **الموقع:** [`pkg/utils/file/copy.go:29-66`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/copy.go#L29-L66) (`ValidateCopyTarget`) و[`route/v1/file.go:688-725`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L688-L725) (`PostOperateFileOrDir`).
- **التفاصيل الفنية:**
  - الدالة `ValidateCopyTarget` تفحص فقط عدم كون المصدر جذر قرص (`filepath.Dir(source) == source`) وتفحص `safefs.NoLinks`، لكنها **لا تفحص `InternalStoragePath(source)` ولا `InternalStoragePath(target)` ولا `ValidateDeletePath(source)`** (عند النقل `move`).
  - يسمح ذلك بنسخ مجلد `C:\ProgramData\CasaOS\conf\auth` أو قاعدة البيانات إلى مجلد عام ثم تنزيله، أو نقل جذر التخزين الافتراضي `C:\CasaOS\DATA` أو مجلدات الخدمة الداخلية مما يعطل الخادم.
- **الحل المطلوب:** رفض أي مصدر أو وجهة يقع ضمن `InternalStoragePath` في `ValidateCopyTarget`، واستدعاء `ValidateDeletePath(source)` عندما تكون العملية `move`.

---

#### الخطأ #7: إمكانية نقل ملفات الخدمة الداخلية أو آباء جذور التخزين إلى سلة المحذوفات عبر `POST /api/trash/move`
- **الموقع:** [`trash_api.go:150-180`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/trash_api.go#L150-L180).
- **التفاصيل الفنية:**
  - يتحقق `/api/trash/move` من عدم كون المسار هو `RecycleBinDir` أو جذر القرص، لكنه **لا يستدعي `file.ValidateDeletePath(source)`**!
  - يمكن نتيجة لذلك إرسال طلب لنقل `C:\ProgramData\CasaOS\conf` أو `C:\ProgramData\CasaOS\db` أو `C:\CasaOS` (الأب الذي يضم `C:\CasaOS\DATA`) إلى سلة المحذوفات.
- **الحل المطلوب:** استدعاء `file.ValidateDeletePath(source)` في بداية معالجة `/api/trash/move`.

---

#### الخطأ #8: غياب فحص المسارات في إنشاء المجلدات `MkdirAll`، وغياب فحص `InternalStoragePath` في الرفع وفك الضغط
- **الموقع:**
  - [`route/v1/file.go:427-446`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L427-L446) و[`service/system.go:197-212`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/system.go#L197-L212) (`MkdirAll`).
  - [`route/v1/upload_chunks.go:18-38`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/upload_chunks.go#L18-L38) (`uploadTargetDir`).
  - [`archive_api.go:225-260`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/archive_api.go#L225-L260) (`handleUnzip`).
- **التفاصيل الفنية:**
  1. `MkdirAll` لا يتحقق من كون المسار مطلقاً (`filepath.IsAbs`)، ولا يفحص `safefs.NoLinks(path)`، ولا يمنع `file.InternalStoragePath(path)`، ولا يمنع مسارات NTFS Alternate Data Streams (`:`).
  2. `uploadTargetDir` و`handleUnzip` يتحققان من `safefs.NoLinks` لكن لا يمنعان الرفع أو فك الضغط داخل المسارات الداخلية (`file.InternalStoragePath`).
- **الحل المطلوب:** إضافة فحص `filepath.IsAbs` و`safefs.NoLinks` و`!file.InternalStoragePath` ومنع `:` على ويندوز في `MkdirAll` و`uploadTargetDir` و`handleUnzip`.

---

#### الخطأ #9: تجاوز حدود تنزيل المجلدات (`10,000` عنصر) عبر مسار التنزيل الجماعي `GET /v1/batch`
- **الموقع:** [`route/v1/file.go:149-223`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L149-L223) (`GetDownloadFile`) و[`pkg/utils/file/file.go:324-377`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/file.go#L324-L377) (`AddFile`).
- **التفاصيل الفنية:**
  - في `GetDownloadFile` (السطر 186):
    ```go
    if !info.IsDir() {
        return serveDownloadFile(ctx, filePath, true)
    }
    ```
    إذا طُلب تنزيل مجلد واحد أو عدة مجلدات عبر `GET /v1/batch?files=C:\Folder`، يتجاوز الكود دالة `downloadDirectory` (التي تطبق سقف 10,000 عنصر وعمق 128 وفحص الإلغاء ومنع جذور الأقراص) ويستدعي `file.AddFile` التي تقرأ كل أسماء الملفات دفعة واحدة (`file.Readdirnames(0)`) بلا أي سقف لعدد العناصر أو العمق أو الحجم، وبلا مراقبة لإلغاء الطلب (`ctx.Request().Context()`), وتسمح بتمرير جذر القرص `C:\`!
- **الحل المطلوب:** إذا كان `len(list) == 1 && info.IsDir()` استدعِ `downloadDirectory(ctx, filePath)` مباشرة، وطبق حدود العداد والعمق والسياق ومنع الجذور على التنزيل المتعدد أيضاً.

---

#### الخطأ #10: مسارات غير محمية بـ JWT في وضع Linux غير المستقل + ثغرة Reflected XSS في `/v1/recover/:type`
- **الموقع:** [`route/v1.go:33-42`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1.go#L33-L42) و[`route/v1/recover.go:19-247`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/recover.go#L19-L247).
- **التفاصيل الفنية:**
  - المسارات `/v1/sys/debug` و`/v1/sys/version/check` و`/v1/recover/:type` مسجلة على `e` خارج مجموعة `v1Group` التي تفرض JWT. في وضع Linux غير المستقل (`-standalone=false`)، يمكن الوصول إليها دون مصادقة.
  - في `GetRecoverStorage` ([`route/v1/recover.go`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/recover.go#L41)) يتم بناء استجابة HTML خام عبر دمج نص الخطأ دون تهريب:
    ```go
    return ctx.HTML(http.StatusOK, `<p>Initialization failure:`+err.Error()+`</p><script>window.close()</script>`)
    ```
    مما يسبب ثغرة XSS منعكس، بالإضافة إلى إمكانية تعديل إعدادات `rclone`.
- **الحل المطلوب:** نقل هذه المسارات إلى داخل `v1Group` المحمي بالمصادقة، واستخدام `html.EscapeString(err.Error())`.

---

#### الخطأ #11: تعارض وظيفي في `ValidateStoragePath` يمنع إنشاء الملفات في الأقراص الأخرى ويرفض `CASAOS_DATA_ROOT` إذا كان جذر قرص
- **الموقع:** [`pkg/utils/file/storage.go:16-46`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/storage.go#L16-L46) و[`pkg/storagepaths/settings.go:52-66`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/storagepaths/settings.go#L52-L66).
- **التفاصيل الفنية:**
  1. يعرض متصفح الملفات (`GetDirPath`) جميع الأقراص المحلية (`C:\`, `D:\`...). عندما يتصفح المستخدم مجلداً عادياً مثل `D:\Projects`، يمكنه رفع ملفات إليه أو إنشاء مجلدات، لكن عند الضغط على "إنشاء ملف" (`POST /v1/file`) يفشل الطلب بـ `400` لأن `PostCreateFile` يشترط أن يكون المسار داخل `storage-paths.json` فقط!
  2. في `WINDOWS.md` يُذكر أنه يمكن ضبط `$env:CASAOS_DATA_ROOT = 'E:\CasaOS\DATA'`، لكن إذا خصص مستخدم قرصاً كاملاً للتخزين مثل `$env:CASAOS_DATA_ROOT = 'E:\'`، فإن `Default()` يضع `DefaultPath = "E:\\"`، وحينها تفشل `storagepaths.Validate` (السطر 58: `if filepath.Dir(*path) == *path`) وتتعطل قراءة الإعدادات والمعرض وإنشاء الملفات بالكامل.
- **الحل المطلوب:** توحيد سياسة مسارات الكتابة بين `PostCreateFile` و`MkdirAll` و`PostFileUpload`، والتحقق من `CASAOS_DATA_ROOT` عند بدء التشغيل.

---

#### الخطأ #12: مشاكل أمان ومطابقة مشاركات SMB على ويندوز ولينكس
- **الموقع:** [`service/shares_windows.go:45-65`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/shares_windows.go#L45-L65) و[`route/v1/samba.go:64-112`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/samba.go#L64-L112).
- **التفاصيل الفنية:**
  1. **صلاحيات مفرطة:** ينشئ `shares_windows.go` المشاركة بـ `-FullAccess $everyone` (`S-1-1-0`)، ويستدعي `samba.go:105` الدالة `os.Chmod(v.Path, 0o777)`، ولا يمنع مشاركة المسارات الداخلية (`InternalStoragePath`).
  2. **فشل تحديث مسار مشاركة مملوكة لـ CasaOS:** في [`service/shares_windows.go:48-51`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/shares_windows.go#L48-L51):
     ```powershell
     $current = $existing | Where-Object Name -eq $share.Name
     if ($current -and ($current.Path -ne $share.Path -or ($current.Description -ne $marker -and $current.Description -ne ('CasaOS share ' + $share.Name)))) {
         throw ('Share name is owned by another application: ' + $share.Name)
     }
     ```
     إذا كانت هناك مشاركة سابقة أنشأها CasaOS نفسه (`$current.Description -eq $marker`) بالاسم `Photos` ولكن بمسار قديم `C:\OldPhotos`، وأراد CasaOS تحديثها إلى `D:\NewPhotos`، فإن الشرط `$current.Path -ne $share.Path` يتحقق ويرمي خطأ زائفاً بأن المشاركة مملوكة لتطبيق آخر بدلاً من إعادة إنشائها بالمسار الجديد!
- **الحل المطلوب:** منع مشاركة المسارات الداخلية (`ValidateDeletePath`)، وتحديث منطق PowerShell ليحذف ويعيد إنشاء المشاركة إذا كانت مملوكة لـ CasaOS وتغير مسارها، وتوفير خيار القراءة فقط (`ReadAccess`) بدلاً من فرض `FullAccess` للجميع.

---

#### الخطأ #13: مسارات مسجلة بمعالج `nil` في `route/v1.go` تسبب Panic عند استدعائها
- **الموقع:** [`route/v1.go:95-96`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1.go#L95-L96).
- **التفاصيل الفنية:**
  ```go
  v1SysGroup.GET("/server-info", nil)
  v1SysGroup.PUT("/server-info", nil)
  ```
  تسجيل مسار في Echo بمعالج `nil` يؤدي إلى Panic (`runtime error: invalid memory address or nil pointer dereference`) عند طلب `GET /v1/sys/server-info` أو `PUT /v1/sys/server-info`.
- **الحل المطلوب:** حذف السطرين غير المستخدمين من `route/v1.go` (حيث إن الواجهة تستخدم `/api/server-info`).

---

#### الخطأ #14: إمكانية إيقاف تشغيل نظام التشغيل بالكامل أو إنهاء عملية الخادم عبر طلبات HTTP
- **الموقع:** [`route/v1/system.go:166-169`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/system.go#L166-L169) (`PostKillCasaOS`) و[`route/v1/system.go:358-366`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/system.go#L358-L366) (`PutSystemState`) و[`service/system_windows.go:123-131`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/system_windows.go#L123-L131).
- **التفاصيل الفنية:**
  - `POST /v1/sys/stop` ينفذ `os.Exit(0)` فوراً (حتى قبل إرسال استجابة HTTP للعميل!).
  - `PUT /v1/sys/state/off` ينفذ `shutdown /s /t 0`، وبما أن الخدمة تعمل بصلاحية `SYSTEM` على ويندوز، فإنه يغلق جهاز الكمبيوتر/الخادم المضيف بالكامل فوراً.
- **الحل المطلوب:** تعطيل إيقاف تشغيل نظام ويندوز المضيف في خادم الملفات (أو جعل هذه الميزة مشروطة بعلم صريح في الإعدادات).

---

### الفئة الثالثة: أخطاء متوسطة الخطورة (P2 — Medium: الأداء، الموارد، والنظافة البرمجية)

#### الخطأ #15: قفل عام واحد (`uploadChunksMutex`) يحجب جميع عمليات الرفع المجزأ أثناء النقل والدمج
- **الموقع:** [`route/v1/file.go:542, 575-649`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L575-L649).
- **التفاصيل الفنية:**
  - في `PostFileUpload` يتم حجز `uploadChunksMutex.Lock()` في السطر 575 ويظل محجوزاً عبر `defer uploadChunksMutex.Unlock()` أثناء:
    1. قراءة الجزء من اتصال HTTP وكتابته للقرص (`writeUploadChunk`).
    2. عند اكتمال آخر جزء (`ready == true`)، فتح ونسخ ودمج جميع الأجزاء (`1..totalChunks`) في الملف النهائي ومزامنته (`staged.Sync()`).
  - أثناء رفع أو دمج ملف كبير (مثلاً 10 GiB)، تتوقف جميع طلبات `GET /v1/file/upload` و`POST /v1/file/upload` لأي عملية رفع مجزأ أخرى على الخادم!
- **الحل المطلوب:** استخدام قفل خاص بكل عملية رفع (`uploadId`) أو قصر القفل على خطوة التحقق والاعتماد فقط بعد اكتمال كتابة الجزء على القرص.

---

#### الخطأ #16: تراكم أجزاء الرفع المتروكة في `.temp` إلى الأبد دون تنظيف تلقائي (Orphaned Chunks Leak)
- **الموقع:** [`route/v1/upload_chunks.go:40-66`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/upload_chunks.go#L40-L66) و[`route/v1/file.go:356`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L356).
- **التفاصيل الفنية:**
  - إذا بدأ المستخدم رفع ملف كبير (مثلاً 20 GiB) ثم أغلق المتصفح أو انقطع الاتصال عند 90%، تبقى الأجزاء في `<targetDir>/.temp/<sha256>` للأبد.
  - المجلد `.temp` مخفي من واجهة المستخدم (`route/v1/file.go:356`)، وحتى لو حاول المستخدم حذفه عبر API فإن `ValidateDeletePath` يرفض حذفه لأن `InternalStoragePath` يعتبر `.temp` مساراً داخلياً!
- **الحل المطلوب:** إضافة مهمة تنظيف دورية تحذف مجلدات `.temp/<hash>` التي لم تتعدل منذ أكثر من 24 ساعة.

---

#### الخطأ #17: استهلاك المعالج والذاكرة في توليد الصور المصغرة (`GetThumbnailByWebPhoto`) دون حد لأبعاد الصورة أو تخزين مؤقت
- **الموقع:** [`pkg/utils/file/image.go:18-88`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/utils/file/image.go#L18-L88) و[`route/v1/file.go:845-865`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L845-L865).
- **التفاصيل الفنية:**
  1. إذا لم تحتوِ الصورة على EXIF Thumbnail (مثل صور PNG وWebP ومعظم الصور المحفوظة من الويب أو الهواتف بعد التعديل)، تستدعي `GetImage` الدالة `imaging.Open(path)` التي تفك ضغط الصورة الكاملة في الذاكرة ثم تعيد تحجيمها بفلتر `Lanczos` المكلف حسابياً في كل مرة تُفتح فيها الصفحة (بدون أي تخزين مؤقت للصور المصغرة على القرص أو الذاكرة).
  2. لا يوجد فحص لأبعاد الصورة (`image.DecodeConfig`) قبل `imaging.Open(path)`؛ فصورة PNG خبيثة أو ضخمة بأبعاد $20000 \times 20000$ بكسل (حجمها المضغوط أقل من 1 MB) تستهلك ~1.6 GiB من الذاكرة عند فك ضغطها (Image Decompression Bomb).
- **الحل المطلوب:** فحص أبعاد الصورة عبر `image.DecodeConfig` قبل فك ضغطها (مثلاً رفض أبعاد تتجاوز $12000 \times 12000$ للصور المصغرة)، وتقييد عدد عمليات تحجيم الصور المتزامنة (Semaphore)، وإضافة تخزين مؤقت للصور المصغرة.

---

#### الخطأ #18: غياب `SetReadLimit` وحقن JSON وطباعة Debug الصينية في WebSocket
- **الموقع:** [`route/v1/file.go:931-1209`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/file.go#L931-L1209) و[`service/socket.go:20-29`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/socket.go#L20-L29).
- **التفاصيل الفنية:**
  1. في `readPump` (السطر 1124) لا يُضبط `c.conn.SetReadLimit`، مما يسمح لعميل بإرسال رسالة WebSocket واحدة بحجم عدة غيغابايت واستنزاف ذاكرة الخادم.
  2. في `ConnectWebSocket` (السطر 943)، تأتي قيمة `c.ID` من [`service.GetPeerId`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/socket.go#L20-L29) التي تقرأ الكوكي `peerid` من الطلب دون التحقق من كونه UUID صالحاً، ثم يُدمج `c.ID` نصياً في السطر 1130 و1153:
     ```go
     c.handler.broadcast <- []byte(`{"type":"peer-left","peerId":"` + c.ID + `"}`)
     ```
     مما يسمح بحقن حقول JSON إضافية في رسائل البث لجميع المتصلين.
  3. وجود أوامر طباعة غير مهنية في الإنتاج باللغة الصينية (`fmt.Println("推送消息", ...)`, `println("消息来了...")`, `fmt.Println(err)`) في الأسطر `995, 1002, 1016, 1109, 1202`.
- **الحل المطلوب:** ضبط `c.conn.SetReadLimit(64 << 10)`، والتحقق من `peerid` أو استخدام `json.Marshal` لبناء رسالة `peer-left`، وحذف جميع عبارات `fmt.Println` / `println`.

---

#### الخطأ #19: تجاهل معامل عدد الأسطر `lineNumber` وقراءة ملف السجل بالكامل في الذاكرة
- **الموقع:** [`service/system.go:450-465`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/service/system.go#L450-L465) (`GetCasaOSLogs`).
- **التفاصيل الفنية:**
  ```go
  func (s *systemService) GetCasaOSLogs(lineNumber int) string {
      ...
      content, err := io.ReadAll(file)
  ```
  المعامل `lineNumber` يُتجاهل تماماً، وتُقرأ كل محتويات ملف السجل في الذاكرة دفعة واحدة.
- **الحل المطلوب:** قراءة آخر `N` بايت/سطر من نهاية الملف فقط (`file.Seek` من النهاية).

---

#### الخطأ #20: مشاكل اتصال SMB الخارجي (`pkg/samba/smaba.go` و`route/v1/samba.go`)
- **الموقع:** [`pkg/samba/smaba.go:20-75`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/samba/smaba.go#L20-L75) و[`route/v1/samba.go:180-252`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/route/v1/samba.go#L180-L252).
- **التفاصيل الفنية:**
  1. يستدعي `net.Dial("tcp", host+":"+port)` بدون مهلة (`net.DialTimeout`) وبدون `net.JoinHostPort(host, port)` (يفشل مع عناوين IPv6 ويعلق الطلب طويلًا عند غياب الخادم).
  2. في `DeleteSambaConnections` على Linux (السطر 231)، يحاول الاتصال بخادم SMB الخارجي لجلب قائمة المشاركات قبل حذف السجل؛ فإذا كان خادم SMB الخارجي متوقفاً عن العمل، يفشل حذف الاتصال بـ `500` ولا يستطيع المستخدم إزالته!
  3. تُخزن كلمة مرور اتصال SMB في جدول `o_connections` بقاعدة بيانات SQLite بالنص الصريح.
- **الحل المطلوب:** استخدام `net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)`، واعتماد `connection.Directories` المحفوظة في القاعدة عند الحذف بدلاً من اشتراط اتصال شبكي ناجح.

---

#### الخطأ #21: غياب حدود الحجم/العدد ومراقبة إلغاء السياق في ضغط الملفات `POST /api/zip`
- **الموقع:** [`archive_api.go:38-195`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/archive_api.go#L38-L195) (`handleZip`).
- **التفاصيل الفنية:**
  - بينما فك الضغط (`handleUnzip`) مقيد بـ `100 GiB` و`100,000` عنصر، فإن إنشاء الأرشيف (`handleZip`) يفحص فقط `len(req.Files) > 10000` للمصادر العليا، لكنه يمر عبر `filepath.Walk` داخل المجلدات الفرعية دون سقف لإجمالي عدد الملفات أو حجمها، ودون فحص `r.Context().Err()` عند إلغاء العميل للطلب.
- **الحل المطلوب:** فحص `r.Context().Err()` داخل حلقة `filepath.Walk` وأثناء `io.Copy`، ووضع سقف لعدد العناصر والحجم الإجمالي.

---

#### الخطأ #22: كتابة مكررة لسجل السلة عند انتهاء التفريغ (`trash_api.go`)
- **الموقع:** [`trash_api.go:268-286`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/trash_api.go#L268-L286).
- **التفاصيل الفنية:**
  - داخل حلقة تفريغ السلة يتم استدعاء `saveTrashManifest(items[i+1:])` (السطر 278)، وعند آخر عنصر يحفظ قائمة فارغة `[]`، ثم بعد انتهاء الحلقة مباشرة (السطر 283) يستدعي `saveTrashManifest([]TrashItem{})` مرة ثانية بلا حاجة (وحتى لو كانت السلة فارغة ابتداءً يعيد كتابة الملف على القرص).
- **الحل المطلوب:** الاكتفاء بالحفظ داخل الحلقة أو الحفظ خارجها فقط إذا كانت هناك حاجة.

---

## 4. فجوات الجاهزية التجارية والمنتج (Commercial Product Gaps)

إلى جانب الأخطاء البرمجية أعلاه، هناك **7 متطلبات أساسية** تفصل النظام الحالي عن منتج تجاري جاهز للعملاء النهائيين:

1. **العمل الكامل دون إنترنت (Offline LAN Independence):**
   - في [`web/index.html:8-10`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/web/index.html#L8-L10) يتم تحميل الخطوط (`IBM Plex Sans Arabic` و`Plus Jakarta Sans`) من `fonts.googleapis.com`. في الشبكات المحلية المعزولة عن الإنترنت، يفشل تحميل الخطوط الخارجية. يجب تضمين ملفات الخطوط محلياً.
2. **تجربة الإعداد الأول (First-Run Onboarding Wizard):**
   - حالياً يطبع الخادم مسار الملف `C:\ProgramData\CasaOS\conf\auth\initial-password.txt`. المجلد `C:\ProgramData` مخفي افتراضياً في ويندوز ومقيد الصلاحيات. المنتج التجاري يحتاج إلى شاشة إعداد أولي في المتصفح (عند الدخول الأول فقط من `localhost`) تتيح للمستخدم تعيين كلمة المرور واختيار مجلد التخزين بسهولة.
3. **التشفير التلقائي (Auto-TLS / Local HTTPS):**
   - الخادم يعمل افتراضياً عبر HTTP على الشبكة المحلية ما لم يُمرر `-tls-cert` و`-tls-key` يدوياً. في منتج تجاري، ينبغي توليد شهادة TLS محلية تلقائياً عند التشغيل الأول وتخزينها في مجلد الإعدادات، مع إتاحة استبدالها بشهادة مخصصة.
4. **التحويل إلى خدمة ويندوز أصلية (Native Windows Service) وحزمة تثبيت موقعة:**
   - يعتمد التشغيل الخلفي حالياً على `Register-ScheduledTask` في [`install-service.ps1`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/install-service.ps1#L65). في المنتجات التجارية على ويندوز، يُفضل دعم واجهة Windows Service Control Manager (`golang.org/x/sys/windows/svc`) مع مثبت رسومي (MSI / Inno Setup)، وسكربت إلغاء تثبيت (`uninstall-service.ps1`)، وتوقيع رقمي (Authenticode Code Signing) لتجنب تحذير Windows SmartScreen.
5. **روابط المشاركة الخارجية/المؤقتة (Scoped Share Links):**
   - حالياً "مشاركة ملف" في الواجهة تنسخ رابط `/v1/file?path=...` الذي يطلب تسجيل الدخول بحساب المسؤول الكامل. يحتاج المنتج التجاري إلى روابط مشاركة برمز مؤقت (`Share Token`) يمنح صلاحية قراءة/تنزيل لعنصر محدد فقط مع تاريخ انتهاء صلاحية وكلمة مرور اختيارية.
6. **استمرارية الجلسات عند إعادة التشغيل (Persistent Sessions):**
   - تُحفظ جلسات الدخول حالياً في الذاكرة فقط (`m.sessions map[string]time.Time` في [`pkg/localauth/auth.go:39`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/pkg/localauth/auth.go#L39))، مما يخرج جميع الأجهزة المتصلة عند أي إعادة تشغيل للخادم أو تحديث للخدمة.
7. **نظافة المستودع وإدارة الإصدارات (Repository & Release Hygiene):**
   - الملف التنفيذي [`casaos-server.exe`](file:///c:/Users/Masoud/Documents/GitHub/CasaOS/casaos-server.exe) متتبع داخل Git، وهناك 21 ملفاً معدلاً و8 ملفات جديدة غير مودعة، إضافة إلى ملفات تقارير التدقيق المؤقتة في الجذر. ينبغي إضافة `*.exe` إلى `.gitignore` وتنظيم المخرجات عبر pipeline بناء رسمي.

---

## 5. خطة العمل المقترحة للإصلاح قبل الإطلاق

| المرحلة | البنود المشمولة | الأولوية |
|---|---|---|
| **المرحلة 1: إغلاق الثغرات الحرجة والعالية (فوراً)** | إصلاح الأخطاء **#1 إلى #14** (إزالة/تحصين مسارات V2 وSSRF و`WsSsh` و`recover`، إصلاح Panic في `DirSizeB` و`route/v1.go`، توحيد فحص `InternalStoragePath` و`ValidateDeletePath` في المحرر والنسخ والسلة والرفع، وإصلاح تنزيل المجلدات في `/v1/batch` ومشاركات SMB). | **إلزامية قبل أي إطلاق (Blocker)** |
| **المرحلة 2: تحسينات الأداء والموارد (قبل الإطلاق العام)** | إصلاح الأخطاء **#15 إلى #22** (تفكيك قفل `uploadChunksMutex`، التنظيف التلقائي لـ `.temp`، حماية وتحجيم الصور المصغرة، وضع `SetReadLimit` وتنظيف WebSocket، وتحديد حجم قراءة السجلات). | **عالية (High)** |
| **المرحلة 3: تجهيزات المنتج التجاري** | استكمال متطلبات القسم 4 (تضمين الخطوط محلياً، معالج الإعداد الأولي، روابط المشاركة المؤقتة، سكربت إلغاء التثبيت، والتوقيع الرقمي). | **متطلبات تجارية (Product)** |
