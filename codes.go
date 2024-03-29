package assets

const (
	RES_UNKNOWN_ROUTE       = 202_001
	RES_MISSING_UP_PARAM    = 202_002
	RES_UNKNOWN_UP_PARAM    = 202_003
	RES_INVALID_XFORM_PARAM = 202_004
	RES_NOT_FOUND_CACHE     = 202_005

	ERR_CONFIG_READ           = 203_001
	ERR_CONFIG_PARSE          = 203_002
	ERR_CONFIG_ZERO_UPSTREAMS = 203_003
	ERR_CONFIG_UPSTREAM_BASE  = 203_004
	ERR_CONFIG_VIPS_PATH      = 203_005
	ERR_CONFIG_VIPS_VERSION   = 203_006
	ERR_PROXY                 = 203_007
	ERR_TRANSFORM             = 203_008
	ERR_LOCAL_IMAGE_MISSING   = 203_009
	ERR_FS_STAT               = 203_010
	ERR_UNCAUGHT_HTTP         = 203_011
)
