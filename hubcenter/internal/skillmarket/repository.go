package skillmarket

import "errors"

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("not found")

// ErrInsufficientCredits 表示 Credits 余额不足。
var ErrInsufficientCredits = errors.New("insufficient credits")

// ErrAlreadyRefunded 表示该购买记录已退款。
var ErrAlreadyRefunded = errors.New("already refunded")

// ErrVoucherExpired 表示体验券已过期。
var ErrVoucherExpired = errors.New("voucher expired")

// ErrVoucherExhausted 表示体验券已用完。
var ErrVoucherExhausted = errors.New("voucher exhausted")

// ErrVoucherNotApplicable 表示体验券不适用（如 Skill 声明了 required_env）。
var ErrVoucherNotApplicable = errors.New("voucher not applicable for skills requiring API keys")

// ErrUnverifiedAccount 表示账户未验证，不允许执行该操作。
var ErrUnverifiedAccount = errors.New("account not verified")

// ErrConcurrentConflict 表示并发冲突（乐观锁失败）。
var ErrConcurrentConflict = errors.New("concurrent conflict")
