from collections import Counter
counts=Counter(input().split())
for word,count in sorted(counts.items(),key=lambda x:(-x[1],x[0])): print(word,count)
