package common

// AdvisoryLock serializes ALL schema mutation -- every register/migrate across
// both entity types takes this one key (as an xact or session lock), so no two schema
// changes run at once. Value is arbitrary (ASCII "VULK") but must stay fixed.
const AdvisoryLock int64 = 0x56554C4B
