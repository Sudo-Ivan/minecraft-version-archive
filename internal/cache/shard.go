package cache

func shardIndex(key string) uint32 {
	var h uint32
	for i := 0; i < len(key); i++ {
		h = h*16777619 ^ uint32(key[i])
	}
	return h
}
